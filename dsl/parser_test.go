package dsl_test

import (
	"strings"
	"testing"

	"github.com/ByLCY/papyrus/dsl"
)

const sampleDSL = `
doc Papyrus v1 {
  meta {
    title: "Invoice"
    keywords: [
      "finance"
      "internal"
    ]
  }

  resources {
    font Body {
      src: "fonts/Inter-Regular.ttf"
    }

    color Accent = #0F62FE
  }

  page A4 portrait margin 18mm {
    flow {
      text Body size 12pt color #333 { "Hello, ${user.name}!" }

      let currency = data.meta.currency

      table data.items {
        columns {
          column 50% {
            header: "Name"
            field: item.name
          }
        }
      }
    }
  }
}
`

func TestParseDocument(t *testing.T) {
	doc, err := dsl.ParseString(sampleDSL)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if doc.Name != "Papyrus" {
		t.Fatalf("expected document name Papyrus, got %s", doc.Name)
	}
	if doc.Version != "v1" {
		t.Fatalf("expected version v1, got %s", doc.Version)
	}

	if len(doc.Sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(doc.Sections))
	}

	meta := doc.Sections[0].Meta
	if meta == nil {
		t.Fatalf("meta section missing")
	}
	if len(meta.Block.Statements) < 2 {
		t.Fatalf("meta statements missing: %+v", meta.Block.Statements)
	}
	title := meta.Block.Statements[0].Assignment
	if title == nil || title.Key != "title" {
		t.Fatalf("expected title assignment, got %+v", meta.Block.Statements[0])
	}
	if got := string(*title.Value.String); got != "Invoice" {
		t.Fatalf("expected title Invoice, got %s", got)
	}

	keywords := meta.Block.Statements[1].Assignment
	if keywords == nil || keywords.Value.Array == nil {
		t.Fatalf("expected keywords array assignment")
	}
	if len(keywords.Value.Array.Values) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(keywords.Value.Array.Values))
	}

	page := doc.Sections[2].Page
	if page == nil {
		t.Fatalf("page section missing")
	}
	if page.Spec.Size != "A4" {
		t.Fatalf("expected page size A4, got %s", page.Spec.Size)
	}
	if len(page.Spec.Params) != 3 {
		t.Fatalf("expected 3 page params, got %d", len(page.Spec.Params))
	}
	if page.Spec.Params[0].Value != "portrait" || page.Spec.Params[2].Value != "18mm" {
		t.Fatalf("unexpected page params: %+v", page.Spec.Params)
	}

	pageFlow := page.Block.Statements[0].Command
	if pageFlow == nil || pageFlow.Name != "flow" {
		t.Fatalf("expected flow command, got %+v", page.Block.Statements[0])
	}
	if len(pageFlow.Block.Statements) < 3 {
		t.Fatalf("flow block missing statements")
	}

	textCmd := pageFlow.Block.Statements[0].Command
	if textCmd == nil || textCmd.Name != "text" {
		t.Fatalf("expected text command, got %+v", pageFlow.Block.Statements[0])
	}
	if len(textCmd.Args) < 2 || textCmd.Args[0].Value != "Body" {
		t.Fatalf("unexpected text args: %+v", textCmd.Args)
	}
	if textCmd.Block == nil || len(textCmd.Block.Statements) == 0 || textCmd.Block.Statements[0].Text == nil {
		t.Fatalf("text command missing literal content")
	}
	if got := string(textCmd.Block.Statements[0].Text.Value); !strings.Contains(got, "${user.name}") {
		t.Fatalf("expected interpolation in text literal, got %s", got)
	}

	letCmd := pageFlow.Block.Statements[1].Command
	if letCmd == nil || letCmd.Name != "let" {
		t.Fatalf("expected let command, got %+v", pageFlow.Block.Statements[1])
	}
	if len(letCmd.Args) < 4 || letCmd.Args[2].Value != "data" {
		t.Fatalf("unexpected let args: %+v", letCmd.Args)
	}

	tableCmd := pageFlow.Block.Statements[2].Command
	if tableCmd == nil || tableCmd.Name != "table" {
		t.Fatalf("expected table command, got %+v", pageFlow.Block.Statements[2])
	}
	if len(tableCmd.Block.Statements) == 0 {
		t.Fatalf("table command missing body")
	}

	columnsCmd := tableCmd.Block.Statements[0].Command
	if columnsCmd == nil || columnsCmd.Name != "columns" {
		t.Fatalf("expected columns command, got %+v", tableCmd.Block.Statements[0])
	}
	columnCmd := columnsCmd.Block.Statements[0].Command
	if columnCmd == nil || columnCmd.Name != "column" {
		t.Fatalf("expected column command, got %+v", columnsCmd.Block.Statements[0])
	}
	if len(columnCmd.Args) == 0 || columnCmd.Args[0].Value != "50%" {
		t.Fatalf("unexpected column args: %+v", columnCmd.Args)
	}

	if len(columnCmd.Block.Statements) < 2 {
		t.Fatalf("column body missing assignments")
	}
	field := columnCmd.Block.Statements[1].Assignment
	if field == nil || field.Value.Expr == nil {
		t.Fatalf("field assignment should capture expression, got %+v", columnCmd.Block.Statements[1])
	}
	if got := tokensToString(field.Value.Expr.Parts); got != "item . name" {
		t.Fatalf("unexpected expression tokens: %s", got)
	}
}

func tokensToString(parts []*dsl.Lexeme) string {
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		values = append(values, p.Value)
	}
	return strings.Join(values, " ")
}
