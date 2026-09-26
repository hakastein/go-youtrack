package youtrack

import (
	"context"
	"net/http"
)

const projectsPlural = "projects"

type ShowProjectOptions struct {
	Fields string
}

type ListProjectsOptions struct {
	Fields string
	Page   Page
}

func (s *ProjectsService) Show(ctx context.Context, code string, opts *ShowProjectOptions) (*Node, error) {
	return result(s.show(ctx, code, optionsOf(opts)))
}

// List is a page of the projects the token sees.
func (s *ProjectsService) List(ctx context.Context, opts *ListProjectsOptions) (*Node, error) {
	return result(s.list(ctx, optionsOf(opts)))
}

func (s *ProjectsService) show(ctx context.Context, code string, opts ShowProjectOptions) (*Node, *Error) {
	c := s.client
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(projectSchema, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	project, fault := c.request(ctx, projectSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return objectNode(project, requested, project.objects[0], nil)
}

func (s *ProjectsService) list(ctx context.Context, opts ListProjectsOptions) (*Node, *Error) {
	c := s.client
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := c.parseFields(projectSchema, opts.Fields)
	if fault != nil {
		return nil, fault
	}
	return c.listPage(ctx, projectsPlural, "[]"+projectSchema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetProjects(ctx, fields, w)
	})
}
