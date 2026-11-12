package lib

type User struct{ Name string }

// Find returns nil, nil when there is no user: callers must check.
func Find(id int) (*User, error) {
	if id == 0 {
		return nil, nil
	}
	return &User{Name: "u"}, nil
}

// Must never returns nil.
func Must(id int) *User { return &User{Name: "m"} }
