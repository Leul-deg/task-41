package inventory

type AllocationResult struct {
	WarehouseCode string
	AllocatedQty  int
	Deterministic bool
}
