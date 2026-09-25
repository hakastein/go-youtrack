package youtrack

import (
	"context"
	"net/http"
)

// Default fields expressions of the project documents.
const (
	ProjectShowFields = "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))"
	ProjectListFields = "shortName,name"
)

// ShowProjectOptions: Fields is a fields= expression, empty for ProjectShowFields and +x for them and x.
type ShowProjectOptions struct {
	Fields string
}

// ListProjectsOptions: Fields is a fields= expression, empty for ProjectListFields and +x for them and x.
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
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	requested, fault := s.client.parseFields(projectSchema, opts.Fields, ProjectShowFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	project, fault := c.request(ctx, projectSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	return objectNode(project, requested, project.objects[0], nil)
}

func (s *ProjectsService) list(ctx context.Context, opts ListProjectsOptions) (*Node, *Error) {
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := s.client.parseFields(projectSchema, opts.Fields, ProjectListFields)
	if fault != nil {
		return nil, fault
	}
	c := s.client
	return c.listPage(ctx, "projects", "[]"+projectSchema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetProjects(ctx, fields, w)
	})
}
