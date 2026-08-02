package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The dump has to work on Windows, because one of the machines this runs on is
// a Windows desktop and the alternative is telling whoever sits at it to
// reconstruct a twenty six argument neo4j-admin invocation by hand.
//
// None of this starts a database. What it checks is the two failure modes that
// only show up on Windows and only after somebody has copied a dump across:
// a batch file the shell will not run, and an argument list that names a file
// the dump does not contain. Both are silent on Linux and both waste an
// afternoon on Windows. The live Bolt path is exercised in CI on Linux, where a
// service container is available, and by hand on the Windows machine.

// loadedFiles pulls the CSV names out of an import script argument list.
var loadedFiles = regexp.MustCompile(`--(?:nodes|relationships)=([A-Za-z0-9_.]+)`)

func TestBothImportScriptsLoadEveryFileTheExportWroteAndNoOther(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}

	written := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".csv" {
			written[e.Name()] = true
		}
	}

	for _, script := range []string{"import.sh", "import.cmd"} {
		b, err := os.ReadFile(filepath.Join(dir, script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}
		loaded := map[string]bool{}
		for _, m := range loadedFiles.FindAllStringSubmatch(string(b), -1) {
			loaded[m[1]] = true
		}
		// A file named and not written stops the import dead, with an error that
		// names the file and not the reason.
		for name := range loaded {
			if !written[name] {
				t.Errorf("%s loads %s, which the export does not write", script, name)
			}
		}
		// A file written and not named is worse, because the import succeeds. The
		// graph simply arrives missing a layer, and the first person to notice is
		// whoever runs a query that returns nothing.
		for name := range written {
			if !loaded[name] {
				t.Errorf("%s does not load %s, so the import would quietly leave it out", script, name)
			}
		}
	}
}

// A provision is a paragraph and a paragraph has newlines in it, so the text
// lands in a quoted CSV field that spans lines. neo4j-admin refuses those
// unless it is told, and it refuses the whole import rather than the row.
//
// This is asserted as text rather than by importing something, because the
// fixture this package exports has one line of text per provision and would
// import cleanly either way. The corpus is what fails, and the corpus is not in
// the repository.
func TestBothImportScriptsAcceptTextThatSpansLines(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, script := range []string{"import.sh", "import.cmd"} {
		b, err := os.ReadFile(filepath.Join(dir, script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}
		if !strings.Contains(string(b), "--multiline-fields=true") {
			t.Errorf("%s does not pass --multiline-fields, so every real dump fails on the first paragraph", script)
		}
	}
}

func TestTheWindowsImportScriptIsSomethingCmdWillRun(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "import.cmd"))
	if err != nil {
		t.Fatalf("read import.cmd: %v", err)
	}
	text := string(b)

	// Every line ends CRLF. A batch file with bare newlines runs on some Windows
	// builds and truncates the last argument on others, which reads as a corrupt
	// dump rather than as a line ending problem.
	for _, line := range strings.Split(strings.TrimSuffix(text, "\r\n"), "\r\n") {
		if strings.Contains(line, "\n") {
			t.Errorf("import.cmd has a line ending in a bare newline: %q", line)
		}
	}
	if !strings.HasPrefix(text, "@echo off\r\n") {
		t.Error("import.cmd does not open the way a batch file does")
	}
	// cmd.exe has no # comment. A hash at the start of a line is a command it
	// will try to run and fail on, loudly, before doing anything useful.
	for _, line := range strings.Split(text, "\r\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("import.cmd has a shell comment cmd.exe cannot read: %q", line)
		}
	}
	// The file names are relative and carry no separator at all, so the script
	// runs from the dump directory on either platform without a path to fix up.
	for _, m := range loadedFiles.FindAllStringSubmatch(text, -1) {
		if strings.ContainsAny(m[1], `/\`) {
			t.Errorf("import.cmd names %s with a path separator, which will not survive the copy", m[1])
		}
	}
}

func TestTheDumpIsReadableWithWhateverSeparatorTheHostUses(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	// The queries are written into a subdirectory, and a path joined with a
	// hardcoded slash reads on Linux and fails on Windows. This test runs on all
	// three platforms in CI, so it is the one that would catch it.
	for _, q := range Questions {
		if _, err := os.Stat(filepath.Join(dir, "queries", q.File)); err != nil {
			t.Errorf("question %d is not in the dump: %v", q.N, err)
		}
	}
	for _, name := range []string{"schema.cypher", "style.grass", "import.sh", "import.cmd"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s is not in the dump: %v", name, err)
		}
	}
}
