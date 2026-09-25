package youtrack

import (
	"context"
	"fmt"
	"net/http"
)

const (
	UserShowFields = "login,fullName,email,banned"
	UserListFields = "login,fullName,banned"
)

const (
	meSchema    = "Me"
	usersPlural = "users"
	fullNameKey = "fullName"
	emailKey    = "email"
	bannedKey   = "banned"
)

// ShowUserOptions: Fields is a fields= expression, empty for UserShowFields and +x for them and x.
type ShowUserOptions struct {
	Fields string
}

// ListUsersOptions: Fields is a fields= expression, empty for UserListFields and +x for them and x.
type ListUsersOptions struct {
	Fields string
	Page   Page
}

type User struct {
	ID       string
	Login    string
	FullName string
	// Empty where the server answers with null.
	Email  string
	Banned bool
}

// Show refuses before the request what the server would read as other than a login (an internal id, a Hub id, me,
// an empty or dot segment) and a full name, since no login holds a space.
func (s *UsersService) Show(ctx context.Context, login string, opts *ShowUserOptions) (*Node, error) {
	return result(s.show(ctx, login, optionsOf(opts)))
}

// List is a page of the users whose login or full name begins with query; an empty query finds every user, and an
// email address matches none.
func (s *UsersService) List(ctx context.Context, query string, opts *ListUsersOptions) (*Node, error) {
	return result(s.list(ctx, query, optionsOf(opts)))
}

func (s *UsersService) Me(ctx context.Context) (*User, error) {
	return result(s.me(ctx))
}

// Find is up to limit of the users List finds for query, DefaultLimit when limit is zero.
func (s *UsersService) Find(ctx context.Context, query string, limit int) ([]User, error) {
	return result(s.find(ctx, query, limit))
}

func (s *UsersService) show(ctx context.Context, login string, opts ShowUserOptions) (*Node, *Error) {
	login, fault := parseLogin(login)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, UserShowFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	users, fault := c.read(ctx, c.spec, userSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetUser(ctx, login, fields)
	})
	if fault != nil {
		return nil, noSuchLogin(login, fault)
	}
	return users[0], nil
}

func (s *UsersService) list(ctx context.Context, query string, opts ListUsersOptions) (*Node, *Error) {
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(opts.Fields, UserListFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	return c.listPage(ctx, c.spec, usersPlural, "[]"+userSchema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetUsers(ctx, query, fields, w)
	})
}

func (s *UsersService) me(ctx context.Context) (*User, *Error) {
	c := s.client
	a, fault := c.request(ctx, c.spec, meSchema, userFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCurrentUser(ctx, fields)
	})
	if fault != nil {
		return nil, fault
	}
	user, fault := readUser(a, a.objects[0])
	if fault != nil {
		return nil, fault
	}
	return &user, nil
}

func (s *UsersService) find(ctx context.Context, query string, limit int) ([]User, *Error) {
	page, fault := Page{Limit: limit}.parse()
	if fault != nil {
		return nil, fault
	}
	c := s.client
	a, fault := c.request(ctx, c.spec, "[]"+userSchema, userFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetUsers(ctx, query, fields, page.window())
	})
	if fault != nil {
		return nil, fault
	}
	if fault := moreThanAsked(usersPlural, page.Limit, page.Limit, len(a.objects)); fault != nil {
		return nil, fault
	}
	users := make([]User, 0, len(a.objects))
	for _, object := range a.objects {
		user, fault := readUser(a, object)
		if fault != nil {
			return nil, fault
		}
		users = append(users, user)
	}
	return users, nil
}

func userFields() []requestedField {
	return []requestedField{{name: idKey}, {name: loginKey}, {name: fullNameKey}, {name: emailKey}, {name: bannedKey}}
}

func readUser(a decodedResponse, object map[string]any) (User, *Error) {
	id, isID := object[idKey].(string)
	login, isLogin := object[loginKey].(string)
	fullName, isName := object[fullNameKey].(string)
	banned, isFlag := object[bannedKey].(bool)
	email, isEmail := readLocalized(object[emailKey])
	if !isID || !isLogin || !isName || !isFlag || !isEmail {
		return User{}, shapeFailure(a.httpResponse, a.body, "a user is not of the shape the specification gives it")
	}
	return User{ID: id, Login: login, FullName: fullName, Email: email, Banned: banned}, nil
}

func noSuchLogin(login string, fault *Error) *Error {
	if fault.Code != CodeNotFound {
		return fault
	}
	message := fmt.Sprintf("the server has no user of the login %s, and %s", quote(login), findByLoginOrName)
	return &Error{Code: fault.Code, Message: message, Details: fault.Details}
}

const findByLoginOrName = "a search of the users by what their login or full name begins with finds the one meant"
