package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const UserShowFields = "login,fullName,email,banned"

const UserListFields = "login,fullName,banned"

func ListUsers(search, expression string, page Page) (Call, *Error) {
	page, fault := page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, UserListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listUsers(ctx, spec, search, requested, page)
	}, nil
}

func (c *Client) listUsers(ctx context.Context, spec *schemas, search string, requested []requestedField, page Page) (*Node, *Error) {
	return c.listPage(ctx, spec, "users", "[]User", requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetUsers(ctx, search, fields, w)
	})
}

func ShowUser(login, expression string) (Call, *Error) {
	login, fault := parseLogin(login)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, UserShowFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.showUser(ctx, spec, login, requested)
	}, nil
}

func (c *Client) showUser(ctx context.Context, spec *schemas, login string, requested []requestedField) (*Node, *Error) {
	users, fault := c.read(ctx, spec, "User", requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetUser(ctx, login, fields)
	})
	if fault != nil {
		return nil, noSuchLogin(login, fault)
	}
	return users[0], nil
}

func noSuchLogin(login string, fault *Error) *Error {
	if fault.Code != CodeNotFound {
		return fault
	}
	message := fmt.Sprintf("the server has no user of the login %s, and %s", quote(login), findByName(login))
	return &Error{Code: fault.Code, Message: message, Details: fault.Details}
}

func findByName(arg string) string {
	quoted := "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	return quote("ytrack user list --query "+quoted) + " finds one by login or name"
}

func CurrentUser(ctx context.Context, c *Client) (*Node, *Error) {
	users, fault := c.read(ctx, loadSchemas(), "Me", []requestedField{{name: loginKey}, {name: "fullName"}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCurrentUser(ctx, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return users[0], nil
}
