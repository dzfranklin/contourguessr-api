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
	"strings"
)

//go:embed data
var dataDir embed.FS

var regionData []json.RawMessage
var pictureData = make(map[string]map[string]json.RawMessage)
var pictureList = make(map[string][]string)

func init() {
	regionFiles, err := fs.ReadDir(dataDir, "data/regions")
	if err != nil {
		log.Fatal(err)
	}
	for _, regionFile := range regionFiles {
		contents, err := fs.ReadFile(dataDir, "data/regions/"+regionFile.Name())
		if err != nil {
			log.Fatal(err)
		}
		var data json.RawMessage
		if err := json.Unmarshal(contents, &data); err != nil {
			log.Fatal(err)
		}
		regionData = append(regionData, data)
	}

	pictureFiles, err := fs.ReadDir(dataDir, "data/pictures")
	if err != nil {
		log.Fatal(err)
	}
	for _, pictureFile := range pictureFiles {
		region := strings.TrimSuffix(pictureFile.Name(), ".ndjson")
		contents, err := fs.ReadFile(dataDir, "data/pictures/"+pictureFile.Name())
		if err != nil {
			log.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(contents))

		var regionPictureList []string
		regionPictures := make(map[string]json.RawMessage)
		for {
			var data json.RawMessage
			if err := dec.Decode(&data); err != nil {
				if err == io.EOF {
					break
				}
				log.Fatal(err)
			}

			var dataWithId struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &dataWithId); err != nil {
				log.Fatal(err)
			}
			id := dataWithId.ID

			regionPictureList = append(regionPictureList, id)
			regionPictures[id] = data
		}
		if len(regionPictureList) == 0 {
			log.Printf("No pictures found for region %s", region)
		}
		pictureData[region] = regionPictures
		pictureList[region] = regionPictureList
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
