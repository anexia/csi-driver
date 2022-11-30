package types

import "errors"

// Components selects which components to enable, bitmask.
type Components uint

const (
	// Controller enables the csi-driver instance to manage volumes in the cloud.
	Controller Components = 1 << iota

	// Node enables the csi-driver instance to make volumes available on the node it is running on.
	Node
)

// ErrInvalidComponents is returned when Set() cannot map the given string to a valid selection of components.
var ErrInvalidComponents = errors.New("invalid components")

// String returns a stringified version of the received Value.
func (m Components) String() string {
	switch m {
	case Controller | Node:
		return "combined"
	case Controller:
		return "controller"
	case Node:
		return "node"
	}

	return ""
}

// Set parses the given string into the received Value.
func (m *Components) Set(v string) error {
	switch v {
	case "combined":
		*m = Controller | Node
	case "controller":
		*m = Controller
	case "node":
		*m = Node
	default:
		return ErrInvalidComponents
	}

	return nil
}

// Has checks if a given component is enabled on the received Value.
func (m Components) Has(v Components) bool {
	return (m & v) != 0
}
