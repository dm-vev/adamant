package input

// Lock represents a client input lock that can be applied to a player to disable specific inputs such as
// camera rotation, movement, jumping, sneaking or mounting/dismounting entities.
//
// Each lock is a value of its own, which the client of a player translates to
// how its protocol locks that input. The zero Lock is no lock.
type Lock struct {
	lock
}

type lock uint8

// The locks, numbered from 1 in the order All returns them.
const (
	camera lock = iota + 1
	movement
	lateralMovement
	sneak
	jump
	mount
	dismount
	moveForward
	moveBackward
	moveLeft
	moveRight
)

// Camera is the lock that disables all camera movement.
func Camera() Lock {
	return Lock{camera}
}

// Movement is the lock that disables all player movement, including jumping and sneaking.
func Movement() Lock {
	return Lock{movement}
}

// LateralMovement is the lock that disables all player movement excluding jumping and sneaking.
func LateralMovement() Lock {
	return Lock{lateralMovement}
}

// Sneak is the lock that disables the player from sneaking.
func Sneak() Lock {
	return Lock{sneak}
}

// Jump is the lock that disables the player from jumping.
func Jump() Lock {
	return Lock{jump}
}

// Mount is the lock that prevents the player from mounting entities.
func Mount() Lock {
	return Lock{mount}
}

// Dismount is the lock that prevents the player from dismounting entities.
func Dismount() Lock {
	return Lock{dismount}
}

// MoveForward is the lock that disables forward movement.
func MoveForward() Lock {
	return Lock{moveForward}
}

// MoveBackward is the lock that disables backward movement.
func MoveBackward() Lock {
	return Lock{moveBackward}
}

// MoveLeft is the lock that disables left strafe movement.
func MoveLeft() Lock {
	return Lock{moveLeft}
}

// MoveRight is the lock that disables right strafe movement.
func MoveRight() Lock {
	return Lock{moveRight}
}

// All returns all the input locks that are available to be applied to a player.
func All() []Lock {
	return []Lock{
		Camera(), Movement(), LateralMovement(), Sneak(), Jump(), Mount(), Dismount(),
		MoveForward(), MoveBackward(), MoveLeft(), MoveRight(),
	}
}
