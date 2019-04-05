package repotracker

import (
	"io"
	"text/template"
)

var funcMap = template.FuncMap{
	"shortHash": func(hash string) string {
		return hash[:6]
	},
}

// {{with .Info}}{{.URL}}{{end}}
var textReportTmpl = template.Must(template.New("").Funcs(funcMap).Parse("" +
	`#{{with .Repo}} {{.URL}} {{end}}
{{- $ := . }}
<details><summary>There are {{.Log | len}} new commits.</summary><p>
{{range .Log}}
- [` + "`{{.Hash | shortHash}}`" + `]({{.Hash | $.CommitURL}}) {{ .Title }}
{{- with .Tags}} (🏷 {{range .}} [{{.}}]({{. | $.TagURL}}){{- end}}){{- end}}
{{- end}}
</p></details>
`))

// TextReport generates a report of commit log according to the template format for a repository.
func TextReport(w io.Writer, cl *Changelog) error {
	return textReportTmpl.Execute(w, cl)
}

type Reporter interface {
	Report(w io.Writer, cl *Changelog) error
}

type textTemplateReporter struct {
	t *template.Template
}

func (tr textTemplateReporter) Report(w io.Writer, cl *Changelog) error {
	return tr.t.Execute(w, cl)
}

var reporters = map[string]Reporter {
	"md": textTemplateReporter{textReportTmpl},
	"gfmd": ...,
}
