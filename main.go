package main

import (
	"context"
	"contourguessr-api/repos"
	"encoding/json"
	"errors"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var repo *repos.Repo

var challengesPerRegionGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: "contourguessr",
		Name:      "challenges_per_region",
		Help:      "Number of challenges partitioned by region",
	},
	[]string{"region"},
)

func main() {
	err := godotenv.Load(".env", ".env.local")
	if err != nil {
		log.Println(err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL not set")
	}

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	db, err := pgxpool.Connect(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}

	repo = repos.New(db)
	repo.WaitUntilReady()

	go updateChallengesPerRegionCounter()

	router := mux.NewRouter()

	router.Use(apiAllowCORSMiddleware)

	router.HandleFunc("/healthz", handleHealthz)
	router.Handle("/metrics", promhttp.Handler())

	router.HandleFunc("/api/v1/region", handleGetRegions).Methods("GET")
	router.HandleFunc("/api/v1/challenge/random", handleGetRandomChallenge).Methods("GET")
	router.HandleFunc("/api/v1/challenge/{id}", handleGetChallenge).Methods("GET")

	addr := host + ":" + port
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, router))
}

func handleGetRegions(w http.ResponseWriter, _ *http.Request) {
	regions := repo.Regions()
	list := make([]repos.Region, 0, len(regions))
	for _, region := range regions {
		list = append(list, region)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleGetRandomChallenge(w http.ResponseWriter, r *http.Request) {
	var regionID *int
	regionS := r.URL.Query().Get("region")
	if regionS == "" {
		regionID = nil
	} else {
		val, err := strconv.Atoi(regionS)
		if err != nil {
			http.Error(w, "invalid region_id", http.StatusBadRequest)
			return
		}
		regionID = &val
	}

	challenge, err := repo.RandomChallenge(regionID)
	if errors.Is(err, repos.NoChallengesAvailableError) {
		http.Error(w, "no challenges available", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(challenge)
}

func handleGetChallenge(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	challenge, err := repo.Challenge(id)
	if errors.Is(err, repos.ChallengeNotFoundError) {
		http.Error(w, "challenge not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(challenge)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func apiAllowCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "*")
		}
		next.ServeHTTP(w, r)
	})
}

func updateChallengesPerRegionCounter() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		counts := repo.ChallengesPerRegion()
		for region, count := range counts {
			challengesPerRegionGauge.WithLabelValues(strconv.Itoa(region)).Set(float64(count))
		}
	}
}
