package youtrack_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/hakastein/go-youtrack"
)

func ExampleNewClient() {
	c, err := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"),
		youtrack.WithMetadataCache(os.TempDir()))
	if err != nil {
		fmt.Println(err)
		return
	}
	me, err := c.Users.Me(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(me.Login)
}

func ExampleIssuesService_Show() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	doc, err := c.Issues.Show(context.Background(), "DEV-1", &youtrack.ShowIssueOptions{
		Fields:   `+customFields(State,"Модуль системы")`,
		Comments: youtrack.LastComments(3),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	if summary, ok := doc.Lookup("summary"); ok {
		fmt.Println(summary.Value())
	}
	written, _ := json.Marshal(doc)
	fmt.Println(string(written))
}

func ExampleIssuesService_List() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	page, err := c.Issues.List(context.Background(), "project: DEV #Unresolved", &youtrack.ListIssuesOptions{
		Page: youtrack.Page{Limit: 20},
		Warn: func(w *youtrack.Warning) { fmt.Println(w.Message) },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	issues, _ := page.Lookup("issues")
	for _, issue := range issues.Items() {
		id, _ := issue.Lookup("idReadable")
		fmt.Println(id.Value())
	}
}

func ExampleIssuesService_Get() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	issue, err := c.Issues.Get(context.Background(), "DEV-13271")
	if err != nil {
		fmt.Println(err)
		return
	}
	if module, ok := issue.Field("Модуль системы"); ok {
		fmt.Println(module.Texts())
	}
	for _, link := range issue.Links {
		if link.Type.Name == "Subtask" && link.Direction == youtrack.Inward && len(link.Issues) > 0 {
			fmt.Println("parent:", link.Issues[0].IDReadable)
		}
	}
}

func ExampleIssuesService_WriteFields() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	issue, err := c.Issues.WriteFields(context.Background(), "DEV-13271", []youtrack.FieldWrite{
		{Name: "Тестирование", Values: []string{"Пройдено"}},
		{Name: "Модуль системы", Values: []string{"Инфраструктура. DevOps", "TMS"}},
		{Name: "Assignee", Clear: true},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(issue.IDReadable)
}

func ExampleIssuesService_Update() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	summary := "Модуль youtrack v0.3.0"
	_, err := c.Issues.Update(context.Background(), "DEV-13272", &youtrack.IssueUpdate{
		Summary:          &summary,
		ClearDescription: true,
		Fields:           []youtrack.FieldWrite{{Name: "State", Values: []string{"In Progress"}}},
	}, nil)
	var failed *youtrack.Error
	switch {
	case err == nil:
	case errors.Is(err, youtrack.ErrNotFound):
		fmt.Println("no such issue")
	case errors.As(err, &failed) && failed.MayHaveWritten():
		fmt.Println("the instance may hold the write:", failed.Message)
	default:
		fmt.Println(err)
	}
}

func ExampleFieldsService_Bundle() {
	c, _ := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
	bundle, err := c.Fields.Bundle(context.Background(), "DEV", "Модуль системы")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, v := range bundle.Values {
		fmt.Println(v.ID, v.Name, v.Archived)
	}
}
