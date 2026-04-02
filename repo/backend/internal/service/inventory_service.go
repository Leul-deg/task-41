package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"meridian/backend/internal/domain/inventory"
)

type InventoryService struct {
	DB *sql.DB
}

type ReservationReleaseResult struct {
	ReservationID string
	ReleasedQty   int
}

func NewInventoryService(db *sql.DB) *InventoryService {
	return &InventoryService{DB: db}
}

func ComputeConfirmationStatus(reserved, confirmed int) (string, int, error) {
	if reserved < 0 || confirmed < 0 || confirmed > reserved {
		return "", 0, errors.New("confirmed quantity out of range")
	}
	if confirmed == reserved {
		return "CONFIRMED", 0, nil
	}
	return "PARTIAL_CONFIRMED", reserved - confirmed, nil
}

func (s *InventoryService) DeterministicAllocate(ctx context.Context, tx *sql.Tx, siteCode, sku string, qty int) (inventory.AllocationResult, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT wp.warehouse_code
		FROM warehouse_priorities wp
		WHERE wp.site_code=$1
		ORDER BY wp.priority_rank ASC
	`, siteCode)
	if err != nil {
		return inventory.AllocationResult{}, err
	}
	priorityWarehouses := []string{}
	for rows.Next() {
		var wh string
		if rows.Scan(&wh) == nil {
			priorityWarehouses = append(priorityWarehouses, wh)
		}
	}
	rows.Close()

	for _, wh := range priorityWarehouses {
		var id string
		var onHand, reserved int
		err := tx.QueryRowContext(ctx, `
			SELECT id::text, on_hand, reserved
			FROM inventory_balances
			WHERE warehouse_code=$1 AND sku=$2
			ORDER BY sub_warehouse_code
			LIMIT 1
			FOR UPDATE
		`, wh, sku).Scan(&id, &onHand, &reserved)
		if err != nil {
			continue
		}
		if onHand-reserved >= qty {
			_, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET reserved=reserved+$2, updated_at=now() WHERE id=$1::uuid`, id, qty)
			if err != nil {
				return inventory.AllocationResult{}, err
			}
			return inventory.AllocationResult{WarehouseCode: wh, AllocatedQty: qty, Deterministic: true}, nil
		}
	}

	return inventory.AllocationResult{}, errors.New("insufficient stock in prioritized warehouses")
}

func (s *InventoryService) CreateLedger(ctx context.Context, tx *sql.Tx, movementType, sku, wh, reason string, qty int, userID string) error {
	if reason == "" {
		return errors.New("reason code required")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_ledger(id, movement_type, sku, quantity, warehouse_code, reason_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7::uuid)
	`, uuid.NewString(), movementType, sku, qty, wh, reason, userID)
	return err
}

func (s *InventoryService) ResolveSafetyStockThreshold(ctx context.Context, userID, siteCode, sku string) (int, error) {
	if strings.TrimSpace(siteCode) == "" {
		siteCode = "SITE-A"
	}

	var threshold int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT ssr.threshold
			FROM safety_stock_rules ssr
			LEFT JOIN roles r ON r.code = ssr.role_code
			LEFT JOIN user_roles ur ON ur.role_id = r.id AND ur.user_id::text = $1
			WHERE ssr.active = true
			  AND (ssr.site_code = $2 OR ssr.site_code IS NULL)
			  AND (ssr.sku = $3 OR ssr.sku IS NULL)
			  AND (ssr.role_code IS NULL OR ur.user_id IS NOT NULL)
			ORDER BY
			  CASE WHEN ssr.role_code IS NOT NULL THEN 0 ELSE 1 END,
			  CASE WHEN ssr.sku IS NOT NULL THEN 0 ELSE 1 END,
			  CASE WHEN ssr.site_code IS NOT NULL THEN 0 ELSE 1 END,
			  ssr.threshold DESC
			LIMIT 1
		), 20)
	`, strings.TrimSpace(userID), siteCode, strings.TrimSpace(sku)).Scan(&threshold)
	if err != nil {
		return 20, err
	}
	return threshold, nil
}

func (s *InventoryService) ReleaseReservationByID(ctx context.Context, tx *sql.Tx, reservationID, reasonCode string) (ReservationReleaseResult, error) {
	var sku, wh string
	var reserved, confirmed, released int
	err := tx.QueryRowContext(ctx, `
		SELECT sku, warehouse_code, reserved_qty, confirmed_qty, released_qty
		FROM inventory_reservations
		WHERE id=$1::uuid
		FOR UPDATE
	`, reservationID).Scan(&sku, &wh, &reserved, &confirmed, &released)
	if err != nil {
		return ReservationReleaseResult{}, err
	}

	releasable := reserved - confirmed - released
	if releasable < 0 {
		releasable = 0
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inventory_reservations
		SET released_qty=released_qty+$2,
		    status=CASE WHEN (released_qty+$2) >= reserved_qty-confirmed_qty THEN 'RELEASED' ELSE status END,
		    updated_at=now()
		WHERE id=$1::uuid
	`, reservationID, releasable); err != nil {
		return ReservationReleaseResult{}, err
	}

	if releasable > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inventory_balances
			SET reserved=GREATEST(reserved-$3, 0), updated_at=now()
			WHERE warehouse_code=$1 AND sku=$2
		`, wh, sku, releasable); err != nil {
			return ReservationReleaseResult{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reservation_events(id, reservation_id, event_type, quantity, reason_code, created_at)
		VALUES ($1,$2::uuid,'RELEASED',$3,$4,now())
	`, uuid.NewString(), reservationID, releasable, strings.TrimSpace(reasonCode)); err != nil {
		return ReservationReleaseResult{}, err
	}

	return ReservationReleaseResult{ReservationID: reservationID, ReleasedQty: releasable}, nil
}

func (s *InventoryService) ReleaseExpiredReservations(ctx context.Context, reasonCode string) (int, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text
		FROM inventory_reservations
		WHERE status IN ('HELD','PARTIAL_CONFIRMED')
		  AND hold_expires_at <= now()
		  AND (reserved_qty - confirmed_qty - released_qty) > 0
		ORDER BY hold_expires_at ASC
		FOR UPDATE
	`)
	if err != nil {
		return 0, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()

	count := 0
	totalReleased := 0
	for _, id := range ids {
		res, err := s.ReleaseReservationByID(ctx, tx, id, reasonCode)
		if err != nil {
			return count, totalReleased, err
		}
		count++
		totalReleased += res.ReleasedQty
	}

	if err := tx.Commit(); err != nil {
		return count, totalReleased, err
	}
	return count, totalReleased, nil
}

func (s *InventoryService) ReleaseReservationsForOrderCancellation(ctx context.Context, orderID, reasonCode string) (int, int, error) {
	if strings.TrimSpace(orderID) == "" {
		return 0, 0, errors.New("order_id required")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text
		FROM inventory_reservations
		WHERE order_id=$1
		  AND status IN ('HELD','PARTIAL_CONFIRMED')
		  AND (reserved_qty - confirmed_qty - released_qty) > 0
		ORDER BY created_at ASC
		FOR UPDATE
	`, orderID)
	if err != nil {
		return 0, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()

	count := 0
	totalReleased := 0
	for _, id := range ids {
		res, err := s.ReleaseReservationByID(ctx, tx, id, reasonCode)
		if err != nil {
			return count, totalReleased, err
		}
		count++
		totalReleased += res.ReleasedQty
	}

	if err := tx.Commit(); err != nil {
		return count, totalReleased, err
	}
	return count, totalReleased, nil
}

func (s *InventoryService) MoveStock(ctx context.Context, movementType, sku, fromWH, toWH, reason string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("quantity must be positive")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if movementType == "INBOUND" {
		res, err := tx.ExecContext(ctx, `
			UPDATE inventory_balances SET on_hand=on_hand+$3, updated_at=now()
			WHERE warehouse_code=$1 AND sku=$2
		`, toWH, sku, qty)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return errors.New("inventory row not found for warehouse+sku")
		}
		if err := s.CreateLedger(ctx, tx, movementType, sku, toWH, reason, qty, userID); err != nil {
			return err
		}
		return tx.Commit()
	}

	if movementType == "OUTBOUND" {
		var onHand, reserved int
		err = tx.QueryRowContext(ctx, `SELECT on_hand, reserved FROM inventory_balances WHERE warehouse_code=$1 AND sku=$2 FOR UPDATE`, fromWH, sku).Scan(&onHand, &reserved)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("inventory row not found for warehouse+sku")
			}
			return err
		}
		if onHand-reserved < qty {
			return errors.New("insufficient available stock")
		}
		res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET on_hand=on_hand-$3, updated_at=now() WHERE warehouse_code=$1 AND sku=$2`, fromWH, sku, qty)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return errors.New("inventory row not found for warehouse+sku")
		}
		if err := s.CreateLedger(ctx, tx, movementType, sku, fromWH, reason, -qty, userID); err != nil {
			return err
		}
		return tx.Commit()
	}

	if movementType == "TRANSFER" {
		if fromWH == toWH {
			return errors.New("from and to warehouse cannot be same")
		}
		var onHand, reserved int
		err = tx.QueryRowContext(ctx, `SELECT on_hand, reserved FROM inventory_balances WHERE warehouse_code=$1 AND sku=$2 FOR UPDATE`, fromWH, sku).Scan(&onHand, &reserved)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("inventory row not found for warehouse+sku")
			}
			return err
		}
		if onHand-reserved < qty {
			return errors.New("insufficient available stock")
		}
		resFrom, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET on_hand=on_hand-$3, updated_at=now() WHERE warehouse_code=$1 AND sku=$2`, fromWH, sku, qty)
		if err != nil {
			return err
		}
		rowsFrom, err := resFrom.RowsAffected()
		if err != nil {
			return err
		}
		if rowsFrom == 0 {
			return errors.New("inventory row not found for warehouse+sku")
		}
		resTo, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET on_hand=on_hand+$3, updated_at=now() WHERE warehouse_code=$1 AND sku=$2`, toWH, sku, qty)
		if err != nil {
			return err
		}
		rowsTo, err := resTo.RowsAffected()
		if err != nil {
			return err
		}
		if rowsTo == 0 {
			return errors.New("inventory row not found for warehouse+sku")
		}
		if err := s.CreateLedger(ctx, tx, "TRANSFER_OUT", sku, fromWH, reason, -qty, userID); err != nil {
			return err
		}
		if err := s.CreateLedger(ctx, tx, "TRANSFER_IN", sku, toWH, reason, qty, userID); err != nil {
			return err
		}
		return tx.Commit()
	}

	return fmt.Errorf("unsupported movement type %s", movementType)
}
