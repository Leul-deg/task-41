package support

import "time"

type EligibilityResult struct {
	ReturnAllowed bool
	RefundOnly    bool
	Reason        string
	Deadline      time.Time
}
