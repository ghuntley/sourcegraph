package graphql

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/log/logtest"

	"github.com/sourcegraph/sourcegraph/internal/codeintel/stores/dbstore"
	"github.com/sourcegraph/sourcegraph/internal/codeintel/symbols/shared"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/dbtest"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/types"
)

func TestRanges(t *testing.T) {
	// Set up mocks
	mockLogger := logtest.Scoped(t)
	mockDB := database.NewDB(mockLogger, dbtest.NewDB(mockLogger, t))
	mockGitServer := gitserver.NewClient(mockDB)
	mockGitserverClient := NewMockGitserverClient()
	mockSvc := NewMockService()

	// Init resolver and set local request context
	resolver := New(mockSvc, 50, &observation.TestContext)
	resolver.SetLocalCommitCache(mockGitserverClient)
	resolver.SetLocalGitTreeTranslator(mockGitServer, &types.Repo{}, mockCommit, mockPath)

	testLocation1 := shared.Location{DumpID: 50, Path: "a.go", Range: testRange1}
	testLocation2 := shared.Location{DumpID: 51, Path: "b.go", Range: testRange2}
	testLocation3 := shared.Location{DumpID: 51, Path: "c.go", Range: testRange1}
	testLocation4 := shared.Location{DumpID: 51, Path: "d.go", Range: testRange2}
	testLocation5 := shared.Location{DumpID: 51, Path: "e.go", Range: testRange1}
	testLocation6 := shared.Location{DumpID: 51, Path: "a.go", Range: testRange2}
	testLocation7 := shared.Location{DumpID: 51, Path: "a.go", Range: testRange3}
	testLocation8 := shared.Location{DumpID: 52, Path: "a.go", Range: testRange4}

	ranges := []shared.CodeIntelligenceRange{
		{Range: testRange1, HoverText: "text1", Definitions: nil, References: []shared.Location{testLocation1}, Implementations: []shared.Location{}},
		{Range: testRange2, HoverText: "text2", Definitions: []shared.Location{testLocation2}, References: []shared.Location{testLocation3}, Implementations: []shared.Location{}},
		{Range: testRange3, HoverText: "text3", Definitions: []shared.Location{testLocation4}, References: []shared.Location{testLocation5}, Implementations: []shared.Location{}},
		{Range: testRange4, HoverText: "text4", Definitions: []shared.Location{testLocation6}, References: []shared.Location{testLocation7}, Implementations: []shared.Location{}},
		{Range: testRange5, HoverText: "text5", Definitions: []shared.Location{testLocation8}, References: nil, Implementations: []shared.Location{}},
	}

	mockSvc.GetRangesFunc.PushReturn(ranges[0:1], nil)
	mockSvc.GetRangesFunc.PushReturn(ranges[1:4], nil)
	mockSvc.GetRangesFunc.PushReturn(ranges[4:], nil)

	uploads := []dbstore.Dump{
		{ID: 50, Commit: "deadbeef", Root: "sub1/"},
		{ID: 51, Commit: "deadbeef", Root: "sub2/"},
		{ID: 52, Commit: "deadbeef", Root: "sub3/"},
		{ID: 53, Commit: "deadbeef", Root: "sub4/"},
	}
	resolver.SetUploadsDataLoader(uploads)

	mockRequest := shared.RequestArgs{
		RepositoryID: 42,
		Commit:       mockCommit,
		Path:         mockPath,
		Line:         10,
		Character:    20,
		Limit:        50,
	}
	adjustedRanges, err := resolver.Ranges(context.Background(), mockRequest, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error querying ranges: %s", err)
	}
	u := storeDumpToSymbolDump(uploads)
	adjustedLocation1 := shared.UploadLocation{Dump: u[0], Path: "sub1/a.go", TargetCommit: "deadbeef", TargetRange: testRange1}
	adjustedLocation2 := shared.UploadLocation{Dump: u[1], Path: "sub2/b.go", TargetCommit: "deadbeef", TargetRange: testRange2}
	adjustedLocation3 := shared.UploadLocation{Dump: u[1], Path: "sub2/c.go", TargetCommit: "deadbeef", TargetRange: testRange1}
	adjustedLocation4 := shared.UploadLocation{Dump: u[1], Path: "sub2/d.go", TargetCommit: "deadbeef", TargetRange: testRange2}
	adjustedLocation5 := shared.UploadLocation{Dump: u[1], Path: "sub2/e.go", TargetCommit: "deadbeef", TargetRange: testRange1}
	adjustedLocation6 := shared.UploadLocation{Dump: u[1], Path: "sub2/a.go", TargetCommit: "deadbeef", TargetRange: testRange2}
	adjustedLocation7 := shared.UploadLocation{Dump: u[1], Path: "sub2/a.go", TargetCommit: "deadbeef", TargetRange: testRange3}
	adjustedLocation8 := shared.UploadLocation{Dump: u[2], Path: "sub3/a.go", TargetCommit: "deadbeef", TargetRange: testRange4}

	expectedRanges := []shared.AdjustedCodeIntelligenceRange{
		{Range: testRange1, HoverText: "text1", Definitions: []shared.UploadLocation{}, References: []shared.UploadLocation{adjustedLocation1}, Implementations: []shared.UploadLocation{}},
		{Range: testRange2, HoverText: "text2", Definitions: []shared.UploadLocation{adjustedLocation2}, References: []shared.UploadLocation{adjustedLocation3}, Implementations: []shared.UploadLocation{}},
		{Range: testRange3, HoverText: "text3", Definitions: []shared.UploadLocation{adjustedLocation4}, References: []shared.UploadLocation{adjustedLocation5}, Implementations: []shared.UploadLocation{}},
		{Range: testRange4, HoverText: "text4", Definitions: []shared.UploadLocation{adjustedLocation6}, References: []shared.UploadLocation{adjustedLocation7}, Implementations: []shared.UploadLocation{}},
		{Range: testRange5, HoverText: "text5", Definitions: []shared.UploadLocation{adjustedLocation8}, References: []shared.UploadLocation{}, Implementations: []shared.UploadLocation{}},
	}
	if diff := cmp.Diff(expectedRanges, adjustedRanges); diff != "" {
		t.Errorf("unexpected ranges (-want +got):\n%s", diff)
	}
}
