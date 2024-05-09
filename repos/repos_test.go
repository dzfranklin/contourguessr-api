package repos

import (
	"context"
	"encoding/xml"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"os"
	"testing"
	"time"
)

func setupRepo(t *testing.T) (*Repo, func()) {
	t.Helper()

	err := godotenv.Load("../.env", "../.env.test.local")
	if err != nil {
		t.Fatal(err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL not set")
	}

	db, err := pgxpool.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}

	repo := New(db)

	teardown := func() {
		t.Helper()
		repo.Close()
	}

	return repo, teardown
}

func TestRegions(t *testing.T) {
	t.Skip("Hits capabilities endpoints")

	repo, teardown := setupRepo(t)
	defer teardown()

	_ = repo.Regions()

	start := time.Now()
	for i := 0; i < 100; i++ {
		_ = repo.Regions()
	}
	elapsed := time.Since(start)
	if elapsed > 1*time.Millisecond {
		t.Fatalf("expected Regions to be fast, took %s", elapsed)
	}

	regions := repo.Regions()

	if len(regions) == 0 {
		t.Fatal("expected regions, got none")
	}
	t.Logf("regions: %d", len(regions))

	for _, region := range regions {
		t.Logf("region: %d %s", region.ID, region.Name)

		if region.Name == "" {
			t.Error("expected region name, got empty")
		}

		if region.MapLayer.CapabilitiesXML == "" {
			t.Error("expected region MapLayer CapabilitiesXML, got empty")
		}

		var dummy struct{}
		if err := xml.Unmarshal([]byte(region.MapLayer.CapabilitiesXML), &dummy); err != nil {
			t.Error("expected valid region MapLayer CapabilitiesXML, got invalid")
		}
	}
}

func TestRandomChallenge(t *testing.T) {
	t.Skip("Hits capabilities endpoints")

	repo, teardown := setupRepo(t)
	defer teardown()

	_, _ = repo.RandomChallenge(nil)

	startTime := time.Now()
	for i := 0; i < 100; i++ {
		_, err := repo.RandomChallenge(nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	if time.Since(startTime) > 1*time.Millisecond {
		t.Fatal("expected RandomChallenge to be fast")
	}
}
