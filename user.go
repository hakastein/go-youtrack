package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
)

// User is a user of the instance.
type User struct {
	ID       string
	Login    string
	FullName string
	Email    string
	Banned   bool
}

// Users finds up to limit users whose login or full name begins with query; an empty query finds any.
func (c *Client) Users(ctx context.Context, query string, limit int) ([]User, error) {
	if limit < 1 || limit > math.MaxInt32 {
		return nil, &ArgumentError{Argument: "limit", Value: strconv.Itoa(limit), Reason: fmt.Sprintf("is not between 1 and %d", math.MaxInt32)}
	}
	a, err := c.read(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiGetUsers(ctx, query, "id,login,fullName,email,banned", int32(limit))
	})
	if err != nil {
		return nil, err
	}
	objects, err := a.list()
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(objects))
	for _, object := range objects {
		id, isID := object[idKey].(string)
		login, isLogin := object[loginKey].(string)
		fullName, isName := object["fullName"].(string)
		banned, isFlag := object["banned"].(bool)
		email, isEmail := readLocalizedName(object["email"])
		if !isID || !isLogin || !isName || !isFlag || !isEmail {
			return nil, a.invalid("a user is not of the shape the specification gives it")
		}
		users = append(users, User{ID: id, Login: login, FullName: fullName, Email: email, Banned: banned})
	}
	return users, nil
}
