package graph

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Answer is what a competency question returned.
//
// Columns are kept in the order the query returns them rather than sorted,
// because the query author put the identifying column first on purpose and a
// map would throw that away.
type Answer struct {
	Question Question
	Params   map[string]any
	Columns  []string
	Rows     [][]string
}

// Ask runs one competency question against a live database.
//
// The point of this is that nobody should have to write Cypher to get an answer
// out of the graph. A person who has just loaded a dump can run one command with
// a question number and read the rows, and the query they just ran is in the
// dump beside the data if they want to change it. Overrides replace the shipped
// defaults one parameter at a time, so asking question 16 about a different
// article does not mean retyping the date and the limit as well.
func Ask(ctx context.Context, target Target, q Question, overrides map[string]any) (Answer, error) {
	params := map[string]any{}
	for k, v := range q.Params {
		params[k] = v
	}
	for k, v := range overrides {
		if _, ok := params[k]; !ok {
			return Answer{}, fmt.Errorf("question %d takes no parameter named %s, it takes %s", q.N, k, strings.Join(paramNames(q), ", "))
		}
		params[k] = v
	}

	driver, err := neo4j.NewDriverWithContext(target.URI, neo4j.BasicAuth(target.User, target.Password, ""))
	if err != nil {
		return Answer{}, fmt.Errorf("connect %s: %w", target.URI, err)
	}
	defer func() { _ = driver.Close(ctx) }()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: target.Database})
	defer func() { _ = session.Close(ctx) }()

	answer := Answer{Question: q, Params: params}
	// The session method rather than the generic helper. neo4j.ExecuteRead is
	// typed on the work function's return, and this one collects into the answer
	// and has nothing to return, which the helper's cast turns into a panic.
	_, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, q.Cypher(), params)
		if err != nil {
			return nil, err
		}
		records, err := result.Collect(ctx)
		if err != nil {
			return nil, err
		}
		if len(records) > 0 {
			answer.Columns = records[0].Keys
		}
		for _, record := range records {
			row := make([]string, 0, len(record.Values))
			for _, v := range record.Values {
				row = append(row, cell(v))
			}
			answer.Rows = append(answer.Rows, row)
		}
		return nil, nil
	})
	if err != nil {
		return Answer{}, fmt.Errorf("question %d: %w", q.N, err)
	}
	return answer, nil
}

// cell renders one returned value as the string a person reads.
//
// A missing value comes back as an empty string rather than the word null,
// because half the columns in these queries are optional matches and a screen
// of the word null reads as an error when it is the ordinary answer.
func cell(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		if value {
			return "yes"
		}
		return "no"
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, cell(item))
		}
		return strings.Join(parts, " | ")
	case map[string]any:
		// A map comes back from the two queries that collect a kind and a text
		// together. Keys are sorted so two runs read the same.
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+cell(value[k]))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(value)
	}
}

// String renders an answer as one block per row.
//
// Not a table. The columns hold Vietnamese legal text and identifiers a hundred
// characters long, and a table of those in an eighty column terminal is a wall
// of wrapped fragments nobody can line up by eye. One label per line reads.
func (a Answer) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. %s\n", a.Question.N, a.Question.Ask)
	if names := paramNames(a.Question); len(names) > 0 {
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%s = %v", name, a.Params[name]))
		}
		fmt.Fprintf(&b, "asked with %s\n", strings.Join(parts, ", "))
	}
	if len(a.Rows) == 0 {
		b.WriteString("\nno rows\n")
		return b.String()
	}
	width := 0
	for _, name := range a.Columns {
		width = max(width, len(name))
	}
	for i, row := range a.Rows {
		fmt.Fprintf(&b, "\n[%d]\n", i+1)
		for j, value := range row {
			name := ""
			if j < len(a.Columns) {
				name = a.Columns[j]
			}
			fmt.Fprintf(&b, "  %-*s  %s\n", width, name, value)
		}
	}
	if len(a.Rows) == 1 {
		b.WriteString("\n1 row\n")
	} else {
		fmt.Fprintf(&b, "\n%d rows\n", len(a.Rows))
	}
	return b.String()
}

// Catalogue lists the questions with the parameters each takes, for somebody
// deciding which one to ask.
func Catalogue() string {
	var b strings.Builder
	for _, q := range Questions {
		fmt.Fprintf(&b, "%2d  %s\n", q.N, q.Ask)
		if names := paramNames(q); len(names) > 0 {
			fmt.Fprintf(&b, "    %s\n", paramList(q))
		}
	}
	return b.String()
}
