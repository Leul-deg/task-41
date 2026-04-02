package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"meridian/backend/internal/service"
)

type InventoryHandler struct {
	DB  *sql.DB
	Svc *service.InventoryService
}

func NewInventoryHandler(db *sql.DB) *InventoryHandler {
	return &InventoryHandler{DB: db, Svc: service.NewInventoryService(db)}
}

type inventoryAccess struct {
	UserID         string
	SiteCode       string
	WarehouseCode  string
	Global         bool
	WarehouseScope bool
}

func (h *InventoryHandler) loadAccess(c *gin.Context) (inventoryAccess, error) {
	access := inventoryAccess{UserID: c.GetString("userID")}
	if strings.TrimSpace(access.UserID) == "" {
		return access, errors.New("missing actor")
	}

	if err := h.DB.QueryRow(`SELECT COALESCE(site_code,'SITE-A'), COALESCE(warehouse_code,'') FROM users WHERE id=$1`, access.UserID).Scan(&access.SiteCode, &access.WarehouseCode); err != nil {
		return access, err
	}

	rows, err := h.DB.Query(`
		SELECT scope, COALESCE(scope_value,'')
		FROM user_roles ur
		JOIN scope_rules sr ON sr.role_id=ur.role_id
		WHERE ur.user_id=$1 AND sr.module='inventory'
	`, access.UserID)
	if err != nil {
		return access, err
	}
	defer rows.Close()

	for rows.Next() {
		var scope, value string
		if rows.Scan(&scope, &value) != nil {
			continue
		}
		s := strings.ToLower(strings.TrimSpace(scope))
		switch s {
		case "global":
			access.Global = true
		case "site":
			if strings.TrimSpace(value) != "" {
				access.SiteCode = strings.TrimSpace(value)
			}
		case "warehouse":
			access.WarehouseScope = true
			if strings.TrimSpace(value) != "" {
				access.WarehouseCode = strings.TrimSpace(value)
			}
		}
	}
	return access, nil
}

func (a inventoryAccess) checkWarehouse(warehouse string) bool {
	if a.Global {
		return true
	}
	if a.WarehouseScope {
		return strings.EqualFold(strings.TrimSpace(warehouse), strings.TrimSpace(a.WarehouseCode))
	}
	return true
}

func (h *InventoryHandler) warehouseInSite(warehouse, siteCode string) bool {
	if strings.TrimSpace(warehouse) == "" || strings.TrimSpace(siteCode) == "" {
		return false
	}
	var count int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM warehouses WHERE code=$1 AND site_code=$2`, warehouse, siteCode).Scan(&count)
	return count > 0
}

func (h *InventoryHandler) canAccessReservation(c *gin.Context, reservationID string, access inventoryAccess) (string, string, int, int, error) {
	var sku, wh string
	var reserved, confirmed int
	err := h.DB.QueryRow(`SELECT sku, warehouse_code, reserved_qty, confirmed_qty FROM inventory_reservations WHERE id=$1::uuid`, reservationID).Scan(&sku, &wh, &reserved, &confirmed)
	if err != nil {
		return "", "", 0, 0, err
	}
	if access.Global {
		return sku, wh, reserved, confirmed, nil
	}
	if access.WarehouseScope && !access.checkWarehouse(wh) {
		return "", "", 0, 0, errors.New("reservation outside warehouse scope")
	}
	if !access.WarehouseScope {
		var siteCode string
		if err := h.DB.QueryRow(`SELECT site_code FROM warehouses WHERE code=$1`, wh).Scan(&siteCode); err != nil {
			return "", "", 0, 0, err
		}
		if !strings.EqualFold(siteCode, access.SiteCode) {
			return "", "", 0, 0, errors.New("reservation outside site scope")
		}
	}
	return sku, wh, reserved, confirmed, nil
}

func (h *InventoryHandler) ListReservations(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	query := `
		SELECT id::text, order_id, sku, warehouse_code, reserved_qty, confirmed_qty, released_qty, status, hold_expires_at, created_at
		FROM inventory_reservations
	`
	args := []any{}
	if !access.Global {
		if access.WarehouseScope {
			query += ` WHERE warehouse_code=$1 `
			args = append(args, access.WarehouseCode)
		} else {
			query += ` WHERE warehouse_code IN (SELECT code FROM warehouses WHERE site_code=$1) `
			args = append(args, access.SiteCode)
		}
	}
	query += `
		ORDER BY created_at DESC
		LIMIT 300
	`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reservations"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id, orderID, sku, wh, status string
		var reserved, confirmed, released int
		var holdUntil, createdAt time.Time
		if rows.Scan(&id, &orderID, &sku, &wh, &reserved, &confirmed, &released, &status, &holdUntil, &createdAt) == nil {
			out = append(out, gin.H{
				"id":              id,
				"order_id":        orderID,
				"sku":             sku,
				"warehouse_code":  wh,
				"reserved_qty":    reserved,
				"confirmed_qty":   confirmed,
				"released_qty":    released,
				"status":          status,
				"hold_expires_at": holdUntil,
				"created_at":      createdAt,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"reservations": out})
}

func (h *InventoryHandler) ListOrders(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	query := `
		SELECT id, customer_ref, site_code, created_at
		FROM orders
	`
	args := []any{}
	if !access.Global {
		query += ` WHERE site_code=$1 `
		args = append(args, access.SiteCode)
	}
	query += ` ORDER BY created_at DESC LIMIT 300 `

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load orders"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id, customerRef, siteCode string
		var createdAt time.Time
		if rows.Scan(&id, &customerRef, &siteCode, &createdAt) == nil {
			out = append(out, gin.H{
				"id":           id,
				"customer_ref": customerRef,
				"site_code":    siteCode,
				"created_at":   createdAt,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": out})
}

func (h *InventoryHandler) GetBalances(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	site := c.Query("site")
	if site == "" {
		site = "SITE-A"
	}
	if !access.Global {
		site = access.SiteCode
	}

	query := `
		SELECT ib.warehouse_code, COALESCE(ib.sub_warehouse_code,''), ib.sku, ib.on_hand, ib.reserved
		FROM inventory_balances ib
		JOIN warehouses w ON w.code=ib.warehouse_code
		WHERE w.site_code=$1
	`
	args := []any{site}
	if !access.Global && access.WarehouseScope {
		query += ` AND ib.warehouse_code=$2 `
		args = append(args, access.WarehouseCode)
	}
	query += ` ORDER BY ib.warehouse_code, ib.sku `

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load balances"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	userID := access.UserID
	for rows.Next() {
		var wh, swh, sku string
		var onHand, reserved int
		if err := rows.Scan(&wh, &swh, &sku, &onHand, &reserved); err == nil {
			threshold, terr := h.Svc.ResolveSafetyStockThreshold(context.Background(), userID, site, sku)
			if terr != nil {
				threshold = 20
			}
			available := onHand - reserved
			out = append(out, gin.H{
				"warehouse":       wh,
				"sub_warehouse":   swh,
				"sku":             sku,
				"on_hand":         onHand,
				"reserved":        reserved,
				"available":       available,
				"safety_stock":    threshold,
				"low_stock_alert": available < threshold,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"balances": out})
}

func (h *InventoryHandler) CreateReservation(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		OrderID  string `json:"order_id"`
		SKU      string `json:"sku"`
		Quantity int    `json:"quantity"`
		SiteCode string `json:"site_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}
	if req.SiteCode == "" {
		req.SiteCode = "SITE-A"
	}
	if !access.Global {
		req.SiteCode = access.SiteCode
	}

	tx, err := h.DB.BeginTx(context.Background(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	alloc, err := h.Svc.DeterministicAllocate(context.Background(), tx, req.SiteCode, req.SKU, req.Quantity)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if access.WarehouseScope && !access.checkWarehouse(alloc.WarehouseCode) {
		c.JSON(http.StatusForbidden, gin.H{"error": "allocation outside warehouse scope"})
		return
	}

	id := uuid.NewString()
	_, err = tx.Exec(`
		INSERT INTO inventory_reservations(id, order_id, sku, warehouse_code, reserved_qty, confirmed_qty, status, hold_expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,0,'HELD',now()+interval '2 hours',now())
	`, id, req.OrderID, req.SKU, alloc.WarehouseCode, alloc.AllocatedQty)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reservation create failed", "detail": err.Error()})
		return
	}

	_, _ = tx.Exec(`INSERT INTO reservation_events(id, reservation_id, event_type, quantity, reason_code, created_at) VALUES ($1,$2,'RESERVED',$3,$4,now())`, uuid.NewString(), id, req.Quantity, "ORDER_HOLD_CREATED")

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reservation commit failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "HELD", "warehouse": alloc.WarehouseCode, "deterministic": alloc.Deterministic})
}

func (h *InventoryHandler) ConfirmReservation(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	var req struct {
		ConfirmedQty int    `json:"confirmed_qty"`
		ReasonCode   string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if strings.TrimSpace(req.ReasonCode) == "" {
		req.ReasonCode = "ORDER_CONFIRMED"
	}

	tx, err := h.DB.BeginTx(context.Background(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	var sku, wh string
	var reserved int
	err = tx.QueryRow(`SELECT sku, warehouse_code, reserved_qty FROM inventory_reservations WHERE id=$1 FOR UPDATE`, id).Scan(&sku, &wh, &reserved)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "reservation not found"})
		return
	}
	if !access.checkWarehouse(wh) {
		c.JSON(http.StatusForbidden, gin.H{"error": "reservation outside warehouse scope"})
		return
	}
	status, released, err := service.ComputeConfirmationStatus(reserved, req.ConfirmedQty)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = tx.Exec(`
		UPDATE inventory_reservations
		SET confirmed_qty=$2, released_qty=$3, status=$4, updated_at=now()
		WHERE id=$1
	`, id, req.ConfirmedQty, released, status)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reservation update failed"})
		return
	}

	_, _ = tx.Exec(`UPDATE inventory_balances SET reserved=reserved-$3, on_hand=on_hand-$2, updated_at=now() WHERE warehouse_code=$1 AND sku=$4`, wh, req.ConfirmedQty, reserved, sku)
	_, _ = tx.Exec(`INSERT INTO reservation_events(id, reservation_id, event_type, quantity, reason_code, created_at) VALUES ($1,$2,'CONFIRMED',$3,$4,now())`, uuid.NewString(), id, req.ConfirmedQty, req.ReasonCode)
	if released > 0 {
		_, _ = tx.Exec(`INSERT INTO reservation_events(id, reservation_id, event_type, quantity, reason_code, created_at) VALUES ($1,$2,'RELEASED',$3,$4,now())`, uuid.NewString(), id, released, "PARTIAL_UNCONFIRMED_RELEASE")
	}

	if err := h.Svc.CreateLedger(context.Background(), tx, "OUTBOUND", sku, wh, req.ReasonCode, -req.ConfirmedQty, c.GetString("userID")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func (h *InventoryHandler) ReleaseReservation(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.ReasonCode) == "" {
		req.ReasonCode = "MANUAL_RELEASE"
	}

	tx, err := h.DB.BeginTx(context.Background(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	var exists bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM inventory_reservations WHERE id=$1::uuid)`, id).Scan(&exists); err != nil || !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "reservation not found"})
		return
	}

	_, wh, _, _, accessErr := h.canAccessReservation(c, id, access)
	if accessErr != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": accessErr.Error()})
		return
	}
	if !access.checkWarehouse(wh) {
		c.JSON(http.StatusForbidden, gin.H{"error": "reservation outside warehouse scope"})
		return
	}

	result, err := h.Svc.ReleaseReservationByID(context.Background(), tx, id, req.ReasonCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "reservation not found"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "reservation not found"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"released": true, "released_qty": result.ReleasedQty, "at": time.Now().UTC()})
}

func (h *InventoryHandler) CancelOrderReservations(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		OrderID    string `json:"order_id"`
		ReasonCode string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if strings.TrimSpace(req.OrderID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id required"})
		return
	}
	if strings.TrimSpace(req.ReasonCode) == "" {
		req.ReasonCode = "ORDER_CANCELED"
	}

	if !access.Global {
		if access.WarehouseScope {
			var blocked bool
			_ = h.DB.QueryRow(`
				SELECT EXISTS(
					SELECT 1
					FROM inventory_reservations
					WHERE order_id=$1
					  AND status IN ('HELD','PARTIAL_CONFIRMED')
					  AND (reserved_qty-confirmed_qty-released_qty) > 0
					  AND warehouse_code <> $2
				)
			`, req.OrderID, access.WarehouseCode).Scan(&blocked)
			if blocked {
				c.JSON(http.StatusForbidden, gin.H{"error": "order has reservations outside warehouse scope"})
				return
			}
		} else {
			var blocked bool
			_ = h.DB.QueryRow(`
				SELECT EXISTS(
					SELECT 1
					FROM inventory_reservations ir
					JOIN warehouses w ON w.code=ir.warehouse_code
					WHERE ir.order_id=$1
					  AND ir.status IN ('HELD','PARTIAL_CONFIRMED')
					  AND (ir.reserved_qty-ir.confirmed_qty-ir.released_qty) > 0
					  AND w.site_code <> $2
				)
			`, req.OrderID, access.SiteCode).Scan(&blocked)
			if blocked {
				c.JSON(http.StatusForbidden, gin.H{"error": "order has reservations outside site scope"})
				return
			}
		}
	}

	count, released, err := h.Svc.ReleaseReservationsForOrderCancellation(context.Background(), req.OrderID, req.ReasonCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":                req.OrderID,
		"released_reservations":   count,
		"released_total_quantity": released,
		"reason_code":             req.ReasonCode,
	})
}

func (h *InventoryHandler) MoveInbound(c *gin.Context) {
	h.move(c, "INBOUND")
}

func (h *InventoryHandler) MoveOutbound(c *gin.Context) {
	h.move(c, "OUTBOUND")
}

func (h *InventoryHandler) MoveTransfer(c *gin.Context) {
	h.move(c, "TRANSFER")
}

func (h *InventoryHandler) move(c *gin.Context, movementType string) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		SKU           string `json:"sku"`
		Quantity      int    `json:"quantity"`
		FromWarehouse string `json:"from_warehouse"`
		ToWarehouse   string `json:"to_warehouse"`
		ReasonCode    string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if strings.TrimSpace(req.ReasonCode) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason code required"})
		return
	}
	if access.WarehouseScope {
		if (movementType == "INBOUND" && !access.checkWarehouse(req.ToWarehouse)) ||
			(movementType == "OUTBOUND" && !access.checkWarehouse(req.FromWarehouse)) ||
			(movementType == "TRANSFER" && (!access.checkWarehouse(req.FromWarehouse) || !access.checkWarehouse(req.ToWarehouse))) {
			c.JSON(http.StatusForbidden, gin.H{"error": "warehouse scope violation"})
			return
		}
	} else if !access.Global {
		if (movementType == "INBOUND" && !h.warehouseInSite(req.ToWarehouse, access.SiteCode)) ||
			(movementType == "OUTBOUND" && !h.warehouseInSite(req.FromWarehouse, access.SiteCode)) ||
			(movementType == "TRANSFER" && (!h.warehouseInSite(req.FromWarehouse, access.SiteCode) || !h.warehouseInSite(req.ToWarehouse, access.SiteCode))) {
			c.JSON(http.StatusForbidden, gin.H{"error": "site scope violation"})
			return
		}
	}

	err = h.Svc.MoveStock(context.Background(), movementType, req.SKU, req.FromWarehouse, req.ToWarehouse, req.ReasonCode, req.Quantity, c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"movement": movementType, "ok": true})
}

func (h *InventoryHandler) CycleCount(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		WarehouseCode string `json:"warehouse_code"`
		SKU           string `json:"sku"`
		CountedQty    int    `json:"counted_qty"`
		ReasonCode    string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if strings.TrimSpace(req.ReasonCode) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason code required"})
		return
	}
	if access.WarehouseScope && !access.checkWarehouse(req.WarehouseCode) {
		c.JSON(http.StatusForbidden, gin.H{"error": "warehouse scope violation"})
		return
	}
	if !access.Global && !access.WarehouseScope && !h.warehouseInSite(req.WarehouseCode, access.SiteCode) {
		c.JSON(http.StatusForbidden, gin.H{"error": "site scope violation"})
		return
	}

	var expected int
	err = h.DB.QueryRow(`SELECT on_hand FROM inventory_balances WHERE warehouse_code=$1 AND sku=$2`, req.WarehouseCode, req.SKU).Scan(&expected)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inventory row not found"})
		return
	}

	variance := req.CountedQty - expected
	varPct := 0.0
	if expected != 0 {
		if variance < 0 {
			varPct = float64(-variance) * 100 / float64(expected)
		} else {
			varPct = float64(variance) * 100 / float64(expected)
		}
	}

	threshold := 10.0
	_ = h.DB.QueryRow(`SELECT COALESCE(threshold_percent,10) FROM cycle_count_variance_rules WHERE sku=$1 LIMIT 1`, req.SKU).Scan(&threshold)
	requireApproval := varPct > threshold

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	var sessionID string
	err = tx.QueryRow(`
		INSERT INTO cycle_count_sessions(warehouse_code, reason_code, status, created_by)
		VALUES ($1,$2,$3,$4::uuid)
		RETURNING id::text
	`, req.WarehouseCode, req.ReasonCode, ternary(requireApproval, "PENDING_APPROVAL", "APPROVED"), c.GetString("userID")).Scan(&sessionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create cycle session"})
		return
	}

	_, _ = tx.Exec(`
		INSERT INTO cycle_count_lines(session_id, sku, expected_qty, counted_qty, variance, variance_percent, require_approval)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7)
	`, sessionID, req.SKU, expected, req.CountedQty, variance, varPct, requireApproval)

	if !requireApproval {
		_, _ = tx.Exec(`UPDATE inventory_balances SET on_hand=$3, updated_at=now() WHERE warehouse_code=$1 AND sku=$2`, req.WarehouseCode, req.SKU, req.CountedQty)
		_ = h.Svc.CreateLedger(context.Background(), tx, "CYCLE_COUNT", req.SKU, req.WarehouseCode, req.ReasonCode, variance, c.GetString("userID"))
	} else {
		_, _ = tx.Exec(`
			INSERT INTO inventory_approvals(approval_type, ref_id, status, requested_by, reason_code)
			VALUES ('CYCLE_VARIANCE',$1,'PENDING',$2::uuid,$3)
		`, sessionID, c.GetString("userID"), req.ReasonCode)
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"session_id": sessionID, "requires_approval": requireApproval, "variance_percent": varPct})
}

func (h *InventoryHandler) ReverseLedger(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	ledgerID := c.Param("id")
	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if strings.TrimSpace(req.ReasonCode) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason code required"})
		return
	}

	var wh string
	if err := h.DB.QueryRow(`SELECT warehouse_code FROM inventory_ledger WHERE id=$1::uuid`, ledgerID).Scan(&wh); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ledger entry not found"})
		return
	}
	if access.WarehouseScope && !access.checkWarehouse(wh) {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("ledger entry outside warehouse scope: %s", wh)})
		return
	}
	if !access.Global && !access.WarehouseScope && !h.warehouseInSite(wh, access.SiteCode) {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("ledger entry outside site scope: %s", wh)})
		return
	}

	actor := c.GetString("userID")
	_, err = h.DB.Exec(`
		INSERT INTO ledger_reversals(id, ledger_id, approver_id, reason_code, created_at)
		VALUES ($1,$2,$3,$4,now())
	`, uuid.NewString(), ledgerID, actor, req.ReasonCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reversal failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"reversed": true})
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
