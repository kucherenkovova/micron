package micron

type state byte

const (
	uninitialized state = iota
	initialized
	running
	stopping
	stopped
)

func (s state) String() string {
	switch s {
	case uninitialized:
		return "UNINITIALIZED"
	case initialized:
		return "INITIALIZED"
	case running:
		return "RUNNING"
	case stopping:
		return "STOPPING"
	case stopped:
		return "STOPPED"
	default:
		return "UNKNOWN"
	}
}
