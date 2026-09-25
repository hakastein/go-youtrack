package youtrack

import "context"

type Call func(ctx context.Context, c *Client) (*Node, *Error)
