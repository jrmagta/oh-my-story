// Package role defines workspace roles and what each may do.
package role

import "fmt"

// Role is ordered: a higher value can do everything a lower one can.
type Role int

const (
	Viewer Role = iota + 1
	Commenter
	Editor
	Admin
	Owner
)

var names = map[Role]string{Viewer: "viewer", Commenter: "commenter", Editor: "editor", Admin: "admin", Owner: "owner"}

func (r Role) String() string { return names[r] }

// Parse accepts the lowercase names stored in workspace_members.role.
func Parse(s string) (Role, error) {
	for r, n := range names {
		if n == s {
			return r, nil
		}
	}
	return 0, fmt.Errorf("unknown role %q", s)
}

func (r Role) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

func (r *Role) UnmarshalText(b []byte) error {
	p, err := Parse(string(b))
	if err != nil {
		return err
	}
	*r = p
	return nil
}

type Action int

const (
	View Action = iota + 1
	Comment
	Edit
	ManageMembers // invite, change roles, remove members
	ManageSettings
	DeleteWorkspace
)

var minimum = map[Action]Role{
	View:            Viewer,
	Comment:         Commenter,
	Edit:            Editor,
	ManageMembers:   Admin,
	ManageSettings:  Admin,
	DeleteWorkspace: Owner,
}

// Can denies an action with no entry in minimum, so a forgotten entry fails closed.
func (r Role) Can(a Action) bool {
	m, ok := minimum[a]
	return ok && r >= m
}
