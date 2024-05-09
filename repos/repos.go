package repos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"
)

type Repo struct {
	db *pgxpool.Pool

	cancelUpdater context.CancelFunc
	initWg        sync.WaitGroup
	closeWg       sync.WaitGroup

	mu                    sync.Mutex
	regions               map[int]Region
	challenges            map[int]*Challenge
	challengesByRegion    map[int][]*Challenge
	regionsWithChallenges []int
}

type Challenge struct {
	ID              string     `json:"id"`
	RegionID        int        `json:"region_id"`
	Lng             float64    `json:"lng"`
	Lat             float64    `json:"lat"`
	Title           string     `json:"title"`
	DescriptionHTML string     `json:"description"`
	DateTaken       *time.Time `json:"date_taken"`
	Link            string     `json:"link"`
	Src             struct {
		Preview PictureSrc `json:"preview"`
		Regular PictureSrc `json:"regular"`
		Large   PictureSrc `json:"large"`
	} `json:"src"`
	Photographer struct {
		Icon string `json:"icon"`
		Text string `json:"text"`
		Link string `json:"link"`
	} `json:"photographer"`
	R struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"r"`
}

type PictureSrc struct {
	Src    string `json:"src"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Region struct {
	ID          int             `json:"id"`
	GeoJSON     json.RawMessage `json:"geo_json"`
	Name        string          `json:"name"`
	CountryISO2 string          `json:"country_iso2"`
	LogoURL     string          `json:"logo_url"`
	MinLng      float64         `json:"min_lng"`
	MaxLng      float64         `json:"max_lng"`
	MinLat      float64         `json:"min_lat"`
	MaxLat      float64         `json:"max_lat"`
	MapLayer    MapLayer        `json:"map_layer"`
}

type MapLayer struct {
	ID                int       `json:"id"`
	Name              string    `json:"name"`
	CapabilitiesXML   string    `json:"capabilities_xml"`
	Layer             string    `json:"layer"`
	MatrixSet         string    `json:"matrix_set"`
	Resolutions       []float64 `json:"resolutions"`
	DefaultResolution float64   `json:"default_resolution"`
	OSBranding        bool      `json:"os_branding"`
	ExtraAttributions []string  `json:"extra_attributions"`
}

var NoChallengesAvailableError = errors.New("no challenges available")
var ChallengeNotFoundError = errors.New("challenge not found")

func New(db *pgxpool.Pool) *Repo {
	updaterCtx, cancelUpdater := context.WithCancel(context.Background())
	r := &Repo{
		db:            db,
		cancelUpdater: cancelUpdater,
	}

	r.initWg.Add(2)
	r.closeWg.Add(2)
	go r.challengesUpdater(updaterCtx)
	go r.regionsUpdater(updaterCtx)

	return r
}

func (r *Repo) WaitUntilReady() {
	r.initWg.Wait()
}

func (r *Repo) Close() {
	log.Println("closing repo")
	r.cancelUpdater()
	r.closeWg.Wait()
	r.db.Close()
}

func (r *Repo) Regions() map[int]Region {
	r.initWg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.regions
}

func (r *Repo) RandomChallenge(region *int) (Challenge, error) {
	r.initWg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()

	var regionID int
	if region != nil {
		regionID = *region
	} else {
		regionID = r.regionsWithChallenges[rand.Intn(len(r.regionsWithChallenges))]
	}

	list := r.challengesByRegion[regionID]
	if len(list) == 0 {
		return Challenge{}, NoChallengesAvailableError
	}

	pick := *list[rand.Intn(len(list))]

	return pick, nil
}

func (r *Repo) Challenge(id string) (Challenge, error) {
	r.initWg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()

	internalID, err := decodeChallengeID(id)
	if err != nil {
		return Challenge{}, err
	}

	val, ok := r.challenges[internalID]
	if !ok {
		return Challenge{}, ChallengeNotFoundError
	}

	return *val, nil
}

func (r *Repo) ChallengesPerRegion() map[int]int {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[int]int)
	for k, v := range r.challengesByRegion {
		out[k] = len(v)
	}
	return out
}

func (r *Repo) regionsUpdater(ctx context.Context) {
	defer r.closeWg.Done()

	err := r.updateRegions(ctx)
	if err != nil {
		log.Fatalf("failed to initially update regions: %v", err)
	}
	r.initWg.Done()

	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			err := r.updateRegions(ctx)
			if err != nil {
				log.Printf("error updating regions: %v", err)
			}
		case <-ctx.Done():
			log.Println("cancelling regions updater")
			return
		}
	}
}

func (r *Repo) updateRegions(ctx context.Context) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, ST_AsGeoJSON(geo), name, country_iso2, logo_url, min_lng, max_lng, min_lat, max_lat
		FROM regions
		WHERE active
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make(map[int]Region)
	for rows.Next() {
		var r Region
		if err := rows.Scan(&r.ID, &r.GeoJSON, &r.Name, &r.CountryISO2, &r.LogoURL, &r.MinLng, &r.MaxLng, &r.MinLat, &r.MaxLat); err != nil {
			return err
		}
		out[r.ID] = r
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		SELECT ml.id, ml.name, ml.capabilities_url, ml.layer, ml.matrix_set, ml.resolutions, ml.default_resolution, ml.os_branding, ml.extra_attributions
		FROM map_layers as ml
		JOIN region_map_layers ON ml.id = region_map_layers.map_layer_id
		JOIN map_layers ON map_layers.id = region_map_layers.map_layer_id
		JOIN regions ON regions.id = region_map_layers.region_id
		WHERE regions.active
		GROUP BY ml.id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	mapLayers := make(map[int]*MapLayer)
	for rows.Next() {
		var ml MapLayer
		var osBranding *bool
		if err := rows.Scan(&ml.ID, &ml.Name, &ml.CapabilitiesXML, &ml.Layer, &ml.MatrixSet, &ml.Resolutions, &ml.DefaultResolution, &osBranding, &ml.ExtraAttributions); err != nil {
			return err
		}
		if osBranding != nil {
			ml.OSBranding = *osBranding
		}
		mapLayers[ml.ID] = &ml
	}
	rows.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex
	c := http.Client{
		Timeout: 10 * time.Second,
	}
	for _, ml := range mapLayers {
		wg.Add(1)
		go func(id int, url string) {
			defer wg.Done()
			xml, err := fetchCapabilities(ctx, &c, url)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("error fetching capabilities for map layer %d from %s: %v", id, url, err)
				delete(mapLayers, id)
			} else {
				mapLayers[id].CapabilitiesXML = xml
			}
		}(ml.ID, ml.CapabilitiesXML)
	}
	wg.Wait()

	rows, err = tx.Query(ctx, `
		SELECT region_id, map_layer_id
		FROM region_map_layers
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var regionID, mlID int
		if err := rows.Scan(&regionID, &mlID); err != nil {
			return err
		}

		prevRegionValue, ok := out[regionID]
		if !ok {
			continue
		}

		ml, ok := mapLayers[mlID]
		if !ok {
			log.Printf("missing map layer %d for region %d", mlID, regionID)
			delete(out, regionID)
			continue
		}

		prevRegionValue.MapLayer = *ml
		out[regionID] = prevRegionValue
	}

	r.mu.Lock()
	r.regions = out
	r.mu.Unlock()
	return nil
}

func fetchCapabilities(ctx context.Context, c *http.Client, url string) (string, error) {
	var out string
	err := backoff.Retry(func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "github.com/dzfranklin/contourguessr")

		log.Printf("fetching capabilities from %s", url)

		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var body string
			if v, err := io.ReadAll(resp.Body); err == nil {
				body = string(v)
			} else {
				body = fmt.Sprintf("<error reading body: %v>", err)
			}
			err = fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, body)
			return err
		}

		xml, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if !utf8.Valid(xml) {
			return errors.New("invalid utf-8")
		}

		out = string(xml)
		return nil
	}, backoff.NewExponentialBackOff(backoff.WithMaxElapsedTime(1*time.Minute)))
	return out, err
}

func (r *Repo) challengesUpdater(ctx context.Context) {
	defer r.closeWg.Done()

	err := r.updateChallenges(ctx)
	if err != nil {
		log.Fatalf("failed to initially update challenges: %v", err)
	}
	r.initWg.Done()

	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			err := r.updateChallenges(ctx)
			if err != nil {
				log.Printf("error updating challenges: %v", err)
			}
		case <-ctx.Done():
			log.Println("cancelling challenges updater")
			return
		}
	}
}

func (r *Repo) updateChallenges(ctx context.Context) error {
	rows, err := r.db.Query(ctx, `
		SELECT id, region_id, ST_X(geo::geometry), ST_Y(geo::geometry), title, description_html, date_taken, link,
			preview_src, preview_width, preview_height, regular_src, regular_width, regular_height, large_src, large_width, large_height,
			photographer_icon, photographer_text, photographer_link,
			rx, ry
		FROM challenges
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	challenges := make(map[int]*Challenge)
	challengesByRegion := make(map[int][]*Challenge)
	for rows.Next() {
		c := new(Challenge)
		var internalID int
		err := rows.Scan(&internalID, &c.RegionID, &c.Lng, &c.Lat, &c.Title, &c.DescriptionHTML, &c.DateTaken, &c.Link,
			&c.Src.Preview.Src, &c.Src.Preview.Width, &c.Src.Preview.Height,
			&c.Src.Regular.Src, &c.Src.Regular.Width, &c.Src.Regular.Height,
			&c.Src.Large.Src, &c.Src.Large.Width, &c.Src.Large.Height,
			&c.Photographer.Icon, &c.Photographer.Text, &c.Photographer.Link,
			&c.R.X, &c.R.Y)
		if err != nil {
			return err
		}
		c.ID = encodeChallengeID(internalID)
		challenges[c.RegionID] = c
		challengesByRegion[c.RegionID] = append(challengesByRegion[c.RegionID], c)
	}

	var regionsWithChallenges []int
	for regionID := range challengesByRegion {
		regionsWithChallenges = append(regionsWithChallenges, regionID)
	}

	r.mu.Lock()
	r.challenges = challenges
	r.challengesByRegion = challengesByRegion
	r.regionsWithChallenges = regionsWithChallenges
	r.mu.Unlock()
	return nil
}
