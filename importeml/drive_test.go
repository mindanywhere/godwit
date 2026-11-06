package importeml

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

// fakeCall answers each chunk: paths in fail are failed, the others imported,
// and records the chunks it saw.
func fakeCall(fail map[string]bool, chunks *[][]string) func([]string) (ImportReport, error) {
	return func(chunk []string) (ImportReport, error) {
		*chunks = append(*chunks, slices.Clone(chunk))
		var rep ImportReport
		for _, p := range chunk {
			if fail[p] {
				rep.Failed++
				rep.Results = append(rep.Results, ImportResult{File: p, Status: ImportFail, Reason: "boom"})
				continue
			}
			rep.Imported++
			rep.Results = append(rep.Results, ImportResult{File: p, Status: Imported})
		}
		return rep, nil
	}
}

func TestDriveJournalsAndResumes(t *testing.T) {
	ctx := context.Background()
	state := filepath.Join(t.TempDir(), "import-state.json")
	paths := []string{"a.eml", "b.eml", "c.eml", "d.eml", "e.eml"}

	var chunks [][]string
	var progress []Progress
	stopped, err := Drive(ctx, Run{
		StatePath: state,
		Paths:     paths,
		Batch:     2,
		Call:      fakeCall(nil, &chunks),
		Progress:  func(pr Progress) { progress = append(progress, pr) },
	})
	if err != nil || stopped {
		t.Fatalf("run: stopped %v, err %v", stopped, err)
	}
	if len(chunks) != 3 || len(chunks[0]) != 2 || len(chunks[2]) != 1 {
		t.Fatalf("chunks %v", chunks)
	}
	last := progress[len(progress)-1]
	if last.Done != 5 || last.Total != 5 || last.Imported != 5 {
		t.Fatalf("final progress %+v", last)
	}
	j, err := LoadJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Done) != 5 {
		t.Fatalf("journal %v", j.Done)
	}

	// A second run of the same paths imports nothing: the journal has them.
	chunks = nil
	progress = nil
	if _, err := Drive(ctx, Run{StatePath: state, Paths: paths, Batch: 2, Call: fakeCall(nil, &chunks),
		Progress: func(pr Progress) { progress = append(progress, pr) }}); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("a resumed run imported %v", chunks)
	}
	if progress[0].Done != 5 || progress[0].Total != 5 {
		t.Fatalf("start progress of a done run %+v", progress[0])
	}
}

func TestDriveRetriesFailed(t *testing.T) {
	ctx := context.Background()
	state := filepath.Join(t.TempDir(), "import-state.json")
	paths := []string{"good.eml", "bad.eml"}
	fail := map[string]bool{"bad.eml": true}

	var chunks [][]string
	if _, err := Drive(ctx, Run{StatePath: state, Paths: paths, Batch: 10, Call: fakeCall(fail, &chunks)}); err != nil {
		t.Fatal(err)
	}
	j, err := LoadJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !j.Done["good.eml"] || j.Done["bad.eml"] {
		t.Fatalf("journal %v", j.Done)
	}

	// The failed file is called again; the imported one is not.
	delete(fail, "bad.eml")
	chunks = nil
	if _, err := Drive(ctx, Run{StatePath: state, Paths: paths, Batch: 10, Call: fakeCall(fail, &chunks)}); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || !slices.Equal(chunks[0], []string{"bad.eml"}) {
		t.Fatalf("retry chunks %v", chunks)
	}
}

func TestDriveStopsBetweenBatches(t *testing.T) {
	ctx := context.Background()
	state := filepath.Join(t.TempDir(), "import-state.json")
	paths := []string{"a.eml", "b.eml", "c.eml", "d.eml"}

	var chunks [][]string
	calls := 0
	stopped, err := Drive(ctx, Run{
		StatePath: state,
		Paths:     paths,
		Batch:     2,
		Call:      fakeCall(nil, &chunks),
		Stop:      func() bool { calls++; return calls > 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("not stopped")
	}
	if len(chunks) != 1 {
		t.Fatalf("kept going after stop: %v", chunks)
	}
	j, err := LoadJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Done) != 2 || !j.Done["a.eml"] || !j.Done["b.eml"] {
		t.Fatalf("journal %v", j.Done)
	}

	// The next run brings the rest, and nothing of the first batch.
	chunks = nil
	if _, err := Drive(ctx, Run{StatePath: state, Paths: paths, Batch: 2, Call: fakeCall(nil, &chunks)}); err != nil {
		t.Fatal(err)
	}
	if flat := slices.Concat(chunks...); !slices.Equal(flat, []string{"c.eml", "d.eml"}) {
		t.Fatalf("resumed chunks %v", chunks)
	}
}

func TestDriveStopsOnContext(t *testing.T) {
	state := filepath.Join(t.TempDir(), "import-state.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var chunks [][]string
	stopped, err := Drive(ctx, Run{StatePath: state, Paths: []string{"a.eml"}, Call: fakeCall(nil, &chunks)})
	if !stopped || err != nil || len(chunks) != 0 {
		t.Fatalf("stopped %v, err %v, chunks %v", stopped, err, chunks)
	}
}

func TestDriveBatchError(t *testing.T) {
	state := filepath.Join(t.TempDir(), "import-state.json")
	want := errors.New("socket gone")
	_, err := Drive(context.Background(), Run{
		StatePath: state,
		Paths:     []string{"a.eml", "b.eml"},
		Batch:     1,
		Call:      func([]string) (ImportReport, error) { return ImportReport{}, want },
	})
	if !errors.Is(err, want) {
		t.Fatalf("err %v", err)
	}
}

func TestLoadJournalMissing(t *testing.T) {
	j, err := LoadJournal(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Done) != 0 {
		t.Fatalf("journal %v", j.Done)
	}
}
