package deploy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The argument list is the whole of what this package produces, and it is the
// whole of what can be wrong with it. Every one of these assertions stands for a
// failure that costs a person on a fresh machine somewhere between a confusing
// error and a silently empty database, so they are asserted here where no
// container runtime is needed rather than found by running it.

func TestLoadRunsTheExportsOwnImportScript(t *testing.T) {
	c := Default()
	c.Export = "/tmp/export"
	steps := c.Load()
	if len(steps) != 2 {
		t.Fatalf("load is %d steps, want the volume and the import", len(steps))
	}
	if !steps[0].Ignore {
		t.Error("creating the volume has to tolerate the volume already existing, or every load after the first fails")
	}
	args := strings.Join(steps[1].Args, " ")
	for _, want := range []string{"run --rm", "-w /import", "sh ./import.sh", c.Image} {
		if !strings.Contains(args, want) {
			t.Errorf("import step is missing %q: %s", want, args)
		}
	}
	if !strings.Contains(args, c.Volume+":/data") {
		t.Errorf("import writes into /data and nothing mounts the volume there: %s", args)
	}
	// The importer is offline by definition and gets no port, no auth and no
	// name. If any of those appear here somebody has confused it with Up.
	if strings.Contains(args, "-p ") || strings.Contains(args, "NEO4J_AUTH") {
		t.Errorf("the offline importer was given server arguments: %s", args)
	}
}

func TestUpSetsTheDatabaseNameThroughConfiguration(t *testing.T) {
	c := Default()
	args := strings.Join(c.Up()[1].Args, " ")
	// Community edition runs one user database and will not create another, so a
	// name set any other way leaves the server up and holding nothing.
	if !strings.Contains(args, "NEO4J_initial_dbms_default__database="+c.Database) {
		t.Errorf("up does not name the database, so the import lands somewhere the server will not read: %s", args)
	}
	if !strings.Contains(args, "NEO4J_AUTH=neo4j/"+c.Password) {
		t.Errorf("up does not set the password: %s", args)
	}
	for _, want := range []string{"7474", "7687"} {
		if !strings.Contains(args, want) {
			t.Errorf("up does not publish port %s: %s", want, args)
		}
	}
	if c.Up()[0].Args[0] != "rm" {
		t.Error("up has to remove a stale container first, or a second up fails on the name")
	}
}

func TestUpHonoursChangedPorts(t *testing.T) {
	c := Default()
	c.HTTPPort, c.BoltPort = 17474, 17687
	args := strings.Join(c.Up()[1].Args, " ")
	// The host side moves and the container side does not. Neo4j inside the
	// container listens on the standard ports whatever the host publishes.
	if !strings.Contains(args, "17474:7474") || !strings.Contains(args, "17687:7687") {
		t.Errorf("changed ports were not mapped host to container: %s", args)
	}
	if c.Browser() != "http://localhost:17474" {
		t.Errorf("browser URL is %s and should follow the HTTP port", c.Browser())
	}
	if got := c.Env()[0][1]; got != "bolt://localhost:17687" {
		t.Errorf("bolt URI is %s and should follow the bolt port", got)
	}
}

func TestDownKeepsTheVolumeAndWipeDoesNot(t *testing.T) {
	c := Default()
	for _, s := range c.Down() {
		if s.Args[0] == "volume" {
			t.Fatal("down removed the volume, which throws away an hour of import a person did not ask to lose")
		}
	}
	var removedVolume bool
	for _, s := range c.Wipe() {
		if s.Args[0] == "volume" && s.Args[1] == "rm" {
			removedVolume = true
		}
	}
	if !removedVolume {
		t.Error("wipe left the volume, so it does not do the one thing it exists for")
	}
}

func TestEnvNamesMatchWhatTheToolReads(t *testing.T) {
	// These four strings are read by graph.TargetFromEnv. A rename on either side
	// produces an authentication failure with nothing in it pointing at the cause,
	// which is why they are asserted rather than documented.
	want := []string{
		"LUATDO_NEO4J_URI",
		"LUATDO_NEO4J_USER",
		"LUATDO_NEO4J_PASSWORD",
		"LUATDO_NEO4J_DATABASE",
	}
	env := Default().Env()
	if len(env) != len(want) {
		t.Fatalf("Env has %d entries, want %d", len(env), len(want))
	}
	for i, name := range want {
		if env[i][0] != name {
			t.Errorf("Env[%d] is %s, want %s", i, env[i][0], name)
		}
	}
}

func TestMountResolvesSymlinksInTheSource(t *testing.T) {
	// This is the failure the first macOS run hit. The runtime there is a virtual
	// machine that shares /private and not /tmp, /tmp is a symlink to
	// /private/tmp, and an unresolved path fails inside the machine with
	// "statfs: no such file or directory" about a directory that is plainly
	// there on the host.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this platform will not make a symlink here: %v", err)
	}
	c := Default()
	c.Export = link
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.mount(); !strings.HasPrefix(got, resolved+":/import") {
		t.Errorf("mount is %q and should name %s, which is where the directory really is", got, resolved)
	}
}

func TestMountPassesThroughAPathThatCannotBeResolved(t *testing.T) {
	c := Default()
	c.Export = filepath.Join(t.TempDir(), "not-there")
	// The runtime's own complaint about a missing directory is more useful than
	// one from here about the step before it.
	if got := c.mount(); !strings.HasPrefix(got, c.Export+":/import") {
		t.Errorf("mount is %q and should have passed the path through unchanged", got)
	}
}

func TestMountRelabelsOnLinuxOnly(t *testing.T) {
	c := Default()
	c.Export = t.TempDir()
	m := c.mount()
	if !strings.Contains(m, ":/import") {
		t.Fatalf("mount is %q and should bind the export at /import", m)
	}
	// SELinux relabelling is a Linux notion. Podman on macOS rejects the suffix
	// on some versions and Windows paths already carry a colon.
	if got, want := strings.HasSuffix(m, ":z"), runtime.GOOS == "linux"; got != want {
		t.Errorf("mount %q has the z suffix %v on %s, want %v", m, got, runtime.GOOS, want)
	}
}

func TestCypherAsksTheContainerRatherThanTheHost(t *testing.T) {
	c := Default()
	args := c.Cypher("RETURN 1")[0].Args
	if args[0] != "exec" || args[1] != c.Container {
		t.Fatalf("cypher does not exec into the container: %v", args)
	}
	if args[len(args)-1] != "RETURN 1" {
		t.Errorf("the query is not the last argument, so a query with a leading dash would be read as a flag: %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-d "+c.Database) {
		t.Errorf("cypher does not select the database, so it queries the default and finds nothing: %s", joined)
	}
}

func TestPublishedDatasetCarriesAChecksum(t *testing.T) {
	d := PublishedDataset()
	if len(d.SHA256) != 64 {
		t.Errorf("the published dataset checksum is %q, which is not a sha256", d.SHA256)
	}
	if !strings.HasPrefix(d.URL, "https://") {
		t.Errorf("the published dataset is served over %s, and this archive gets unpacked and executed", d.URL)
	}
	if !strings.Contains(d.URL, DatasetVersion) {
		t.Errorf("the URL %s does not name version %s, so the constant and the file can drift apart", d.URL, DatasetVersion)
	}
}

func TestDatasetOverrideDropsThePublishedChecksum(t *testing.T) {
	t.Setenv("LUATDO_DATASET_URL", "https://mirror.example/luatdo.tar.gz")
	d := PublishedDataset()
	if d.URL != "https://mirror.example/luatdo.tar.gz" {
		t.Fatalf("the override was ignored, URL is %s", d.URL)
	}
	// Carrying the published checksum over to a different file would either
	// reject every mirror or, if somebody removed the check to make mirrors work,
	// silently accept anything.
	if d.SHA256 == DatasetSHA256 {
		t.Error("a mirrored URL kept the published checksum, which vouches for a file nobody has seen")
	}
	if d.Bytes != 0 {
		t.Error("a mirrored URL kept the published size, so progress would be reported against the wrong total")
	}
}
