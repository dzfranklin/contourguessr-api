package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
)

var appEnv = os.Getenv("APP_ENV")

//go:embed regions
var regionsEmbed embed.FS

var regionData []json.RawMessage
var pictureData = make(map[string]map[string]json.RawMessage)
var pictureList = make(map[string][]string)

func init() {
	dataDir := os.Getenv("DATA_DIR")
	if appEnv == "dev" {
		if dataDir == "" {
			dataDir = "./sample_data"
		}
	}
	if dataDir == "" {
		log.Fatal("DATA_DIR not set")
	}

	regionSet := make(map[string]struct{})
	regionFiles, err := fs.ReadDir(regionsEmbed, "regions")
	if err != nil {
		log.Fatal(err)
	}
	for _, regionFile := range regionFiles {
		contents, err := fs.ReadFile(regionsEmbed, "regions/"+regionFile.Name())
		if err != nil {
			log.Fatal(err)
		}

		var parsed struct {
			Id string `json:"id"`
		}
		if err := json.Unmarshal(contents, &parsed); err != nil {
			log.Fatal(err)
		}
		regionSet[parsed.Id] = struct{}{}
		pictureList[parsed.Id] = make([]string, 0)
		pictureData[parsed.Id] = make(map[string]json.RawMessage)

		var data json.RawMessage
		if err := json.Unmarshal(contents, &data); err != nil {
			log.Fatal(err)
		}
		regionData = append(regionData, data)
	}
	sort.Slice(regionData, func(i, j int) bool {
		var a, b struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(regionData[i], &a); err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(regionData[j], &b); err != nil {
			log.Fatal(err)
		}
		return a.Name < b.Name
	})

	picturesFile, err := os.ReadFile(dataDir + "/pictures.ndjson")
	if err != nil {
		log.Fatal("Failed to read pictures.ndjson", err)
	}
	dec := json.NewDecoder(bytes.NewReader(picturesFile))
	for {
		var data json.RawMessage
		if err := dec.Decode(&data); err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}

		var parsed struct {
			Region string `json:"region"`
			ID     string `json:"id"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			log.Fatal(err)
		}

		if _, ok := regionSet[parsed.Region]; !ok {
			log.Printf("Skipping picture %s in unknown region %s", parsed.ID, parsed.Region)
			continue
		}

		pictureList[parsed.Region] = append(pictureList[parsed.Region], parsed.ID)
		pictureData[parsed.Region][parsed.ID] = data
	}
}

func main() {
	log.Print("Starting server...")

	log.Print("Available regions:")
	for _, data := range regionData {
		var region struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &region); err != nil {
			log.Fatal(err)
		}
		log.Printf("  %s: %d pictures", region.Name, len(pictureList[region.Id]))
	}
	log.Print("")

	host := os.Getenv("HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port
	log.Println("Listening on", addr)

	mux := http.NewServeMux()

	mux.Handle("GET /api/v1/region", http.HandlerFunc(RegionListHandler))
	mux.Handle("GET /api/v1/picture/{region}/{id}", http.HandlerFunc(PictureHandler))

	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	mux.HandleFunc("/", NotFoundHandler)

	err := http.ListenAndServe(addr, mux)
	if err != nil {
		panic(err)
	}
}

func RegionListHandler(w http.ResponseWriter, r *http.Request) {
	AllowCORS(w, r)
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(regionData); err != nil {
		log.Fatal(err)
	}
}

func PictureHandler(w http.ResponseWriter, r *http.Request) {
	AllowCORS(w, r)
	region := r.PathValue("region")
	id := r.PathValue("id")
	if region == "" || id == "" {
		NotFoundHandler(w, r)
	}

	if id == "random" {
		pictures := pictureList[region]
		if len(pictures) == 0 {
			NotFoundHandler(w, r)
			return
		}
		id = pictures[rand.Intn(len(pictures))]
		w.Header().Set("Location", "/api/v1/picture/"+region+"/"+id)
		w.WriteHeader(http.StatusFound)
		return
	}

	data, ok := pictureData[region][id]
	if !ok {
		NotFoundHandler(w, r)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func AllowCORS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
}

func NotFoundHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("404 Not Found"))
}
