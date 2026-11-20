package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// runExport saves the visible table, with its filter and sort, as CSV or
// JSON: ":export [csv|json] [file]". It never overwrites a file.
func runExport(a *App, args []string) (tea.Cmd, error) {
	v := a.views[a.tab]
	ex, ok := v.(views.Exporter)
	if !ok {
		return nil, errors.New("this tab has no table to export")
	}
	format := "csv"
	if len(args) > 0 && (args[0] == "csv" || args[0] == "json") {
		format, args = args[0], args[1:]
	}
	path := fmt.Sprintf("sdash-%s-%s.%s", v.Name(), a.now().Format("20060102-150405"), format)
	if len(args) > 0 {
		path = strings.Join(args, " ")
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		path = filepath.Join(a.opt.Getenv("HOME"), rest)
	}
	header, rows := ex.ExportTable()
	data, err := encodeTable(format, header, rows)
	if err != nil {
		return nil, err
	}
	n := len(rows)
	// The write happens off the key handler, so a slow filesystem cannot
	// freeze the screen.
	return func() tea.Msg {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return views.FlashMsg{Text: "export: " + err.Error(), Err: true}
		}
		_, werr := f.Write(data)
		if err := errors.Join(werr, f.Close()); err != nil {
			return views.FlashMsg{Text: "export: " + err.Error(), Err: true}
		}
		return views.FlashMsg{Text: fmt.Sprintf("Saved %d rows to %s", n, path)}
	}, nil
}

func encodeTable(format string, header []string, rows [][]string) ([]byte, error) {
	var b bytes.Buffer
	if format == "json" {
		objs := make([]map[string]string, 0, len(rows))
		for _, r := range rows {
			o := make(map[string]string, len(header))
			for i, h := range header {
				key := strings.ToLower(strings.Join(strings.Fields(h), "_"))
				if key == "" {
					key = fmt.Sprintf("col%d", i+1)
				}
				o[key] = r[i]
			}
			objs = append(objs, o)
		}
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		err := enc.Encode(objs)
		return b.Bytes(), err
	}
	w := csv.NewWriter(&b)
	if err := w.Write(header); err != nil {
		return nil, err
	}
	safe := make([][]string, len(rows))
	for i, r := range rows {
		safe[i] = make([]string, len(r))
		for j, c := range r {
			safe[i][j] = textsafe.CSVCell(c)
		}
	}
	if err := w.WriteAll(safe); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// runNode opens a node's card: ":node NAME", or with no name the first
// node of the job whose card is open (or under the cursor).
func runNode(a *App, args []string) (tea.Cmd, error) {
	if len(args) > 0 {
		return views.Emit(views.NodeMsg{Name: args[0]}), nil
	}
	cur, ok := a.views[a.tab].(views.JobPicker)
	if !ok {
		return nil, errors.New("usage: :node NAME, or open a job first")
	}
	j, ok := cur.CurrentJob()
	if !ok || len(j.NodeList) == 0 {
		return nil, errors.New("this job runs on no node yet")
	}
	return views.Emit(views.NodeMsg{Name: j.NodeList[0]}), nil
}

func completeNodes(a *App, arg int, _ string) []candidate {
	if arg > 0 {
		return nil
	}
	out := make([]candidate, 0, len(a.st.Nodes.Data))
	for _, n := range a.st.Nodes.Data {
		out = append(out, candidate{Value: n.Name, Desc: strings.ToLower(n.State)})
	}
	return out
}
