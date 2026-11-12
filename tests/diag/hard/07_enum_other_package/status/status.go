package status

type Status int

const (
	Pending Status = iota
	Active
	Suspended
	Closed
)

func (s Status) String() string {
	return [...]string{"pending", "active", "suspended", "closed"}[s]
}
