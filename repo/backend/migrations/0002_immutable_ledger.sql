CREATE OR REPLACE FUNCTION prevent_inventory_ledger_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'inventory_ledger is immutable; use reversal entries';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_inventory_ledger_no_update ON inventory_ledger;
CREATE TRIGGER trg_inventory_ledger_no_update
BEFORE UPDATE ON inventory_ledger
FOR EACH ROW EXECUTE FUNCTION prevent_inventory_ledger_mutation();

DROP TRIGGER IF EXISTS trg_inventory_ledger_no_delete ON inventory_ledger;
CREATE TRIGGER trg_inventory_ledger_no_delete
BEFORE DELETE ON inventory_ledger
FOR EACH ROW EXECUTE FUNCTION prevent_inventory_ledger_mutation();
