package main

import (
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/omakoto/mlib"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	youtube "google.golang.org/api/youtube/v3"
)

var (
	clientID     = flag.String("clientid", "", "OAuth 2.0 Client ID.  If non-empty, overrides --clientid_file")
	clientIDFile = flag.String("clientid-file", "clientid.dat",
		"Name of a file containing just the project's OAuth 2.0 Client ID from https://developers.google.com/console.")
	secret     = flag.String("secret", "", "OAuth 2.0 Client Secret.  If non-empty, overrides --secret_file")
	secretFile = flag.String("secret-file", "clientsecret.dat",
		"Name of a file containing just the project's OAuth 2.0 Client Secret from https://developers.google.com/console.")
	cacheToken  = flag.Bool("cachetoken", true, "cache the OAuth 2.0 token")
	deleteID    = flag.String("deleteid", "", "Video delete")
	filename    = flag.String("filename", "", "Name of video file to upload")
	title       = flag.String("title", "Test Title", "Video title")
	description = flag.String("description", "Test Description", "Video description")
	category    = flag.String("category", "22", "Video category")
	keywords    = flag.String("keywords", "", "Comma separated list of video keywords")
	privacy     = flag.String("privacy", "public", "Video privacy status")
	playlist    = flag.String("playlist", "", "Playlist name to add video to")

	playlistsidslice []string
)

func main() {
	flag.Parse()

	config := &oauth2.Config{
		ClientID:     valueOrFileContents(*clientID, *clientIDFile),
		ClientSecret: valueOrFileContents(*secret, *secretFile),
		Endpoint:     google.Endpoint,
		Scopes:       []string{youtube.YoutubeScope, youtube.YoutubepartnerScope, youtube.YoutubeForceSslScope},
	}

	ctx := context.Background()
	client := newOAuthClient(ctx, config)

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("Error creating YouTube client: %v", err)
	}

	if *deleteID != "" {
		playlistIdslice := findPlaylist(service, *playlist)
		nextPageToken := ""
		for i := range playlistIdslice {
			for {
				// Call the playlistItems.list method to retrieve the
				// list of uploaded videos. Each request retrieves 50
				// videos until all videos have been retrieved.
				playlistCall := service.PlaylistItems.List("snippet").
					PlaylistId(playlistIdslice[i]).
					MaxResults(50).
					PageToken(nextPageToken)

				playlistResponse, err := playlistCall.Do()

				if err != nil {
					// The playlistItems.list method call returned an error.
					log.Fatalf("Error fetching playlist items: %v", err.Error())
				}

				for _, playlistItem := range playlistResponse.Items {
					playlistItemID := playlistItem.Id
					videoID := playlistItem.Snippet.ResourceId.VideoId

					if *deleteID == videoID {
						delCall := service.PlaylistItems.Delete(playlistItemID)

						if delCall.Do() != nil {
							log.Fatalf("Error delete for Playlists element. %s", delCall.Do())
						}
						log.Println("Delete Video from Playlist")
					}
				}

				// Set the token to retrieve the next page of results
				// or exit the loop if all results have been retrieved.
				// nextPageToken = playlistResponse.NextPageToken
				if nextPageToken == "" {
					break
				}
			}
		}

		del := service.Videos.Delete(*deleteID)
		if del.Do() != nil {
			log.Fatalf("Error delete YouTube : %v", del.Do())
		}
		log.Println("Delete Video")

	} else {

		if *filename == "" {
			log.Fatalf("You must provide a filename of a video file to upload")
		}

		upload := &youtube.Video{
			Snippet: &youtube.VideoSnippet{
				Title:       *title,
				Description: *description,
				CategoryId:  *category,
			},
			Status: &youtube.VideoStatus{PrivacyStatus: *privacy},
		}

		// The API returns a 400 Bad Request response if tags is an empty string.
		if strings.Trim(*keywords, "") != "" {
			upload.Snippet.Tags = strings.Split(*keywords, ",")
		}

		call := service.Videos.Insert("snippet,status", upload)

		file, err := os.Open(*filename)
		defer file.Close()
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}

		response, err := call.Media(file).Do()
		if err != nil {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
		fmt.Printf("Upload successful! Video ID: %v\n", response.Id)

		if *playlist != "" {
			playlistIdslice := findPlaylist(service, *playlist)
			fmt.Println(playlistIdslice)
			fmt.Println(len(playlistIdslice))
			if len(playlistIdslice) != 0 {
				log.Printf("Playlist found: %s\n", playlistIdslice[0])
			} else {
				playlistIdslice = createPlaylist(service, *playlist)
				log.Printf("Playlist created: id=%s", playlistIdslice[0])
			}
			addToPlaylist(service, response.Id, playlistIdslice)
			log.Printf("Video added to playlist")
		}
	}
}

func newOAuthClient(ctx context.Context, config *oauth2.Config) *http.Client {
	cacheFile := tokenCacheFile(config)
	token, err := tokenFromFile(cacheFile)
	if err != nil {
		token = tokenFromWeb(ctx, config)
		saveToken(cacheFile, token)
	} else {
		log.Printf("Using cached token %#v from %q", token, cacheFile)
	}

	return config.Client(ctx, token)
}

func saveToken(file string, token *oauth2.Token) {
	f, err := os.Create(file)
	if err != nil {
		log.Printf("Warning: failed to cache oauth token: %v", err)
		return
	}
	defer f.Close()
	gob.NewEncoder(f).Encode(token)
}

func valueOrFileContents(value string, filename string) string {
	if value != "" {
		return value
	}
	slurp, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading %q: %v", filename, err)
	}
	return strings.TrimSpace(string(slurp))
}

func tokenCacheFile(config *oauth2.Config) string {
	hash := fnv.New32a()
	hash.Write([]byte(config.ClientID))
	hash.Write([]byte(config.ClientSecret))
	hash.Write([]byte(strings.Join(config.Scopes, " ")))
	fn := fmt.Sprintf("go-api-youtube-tok%v", hash.Sum32())
	return filepath.Join(osUserCacheDir(), url.QueryEscape(fn))
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	if !*cacheToken {
		return nil, errors.New("--cachetoken is false")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := new(oauth2.Token)
	err = gob.NewDecoder(f).Decode(t)
	return t, err
}

func tokenFromWeb(ctx context.Context, config *oauth2.Config) *oauth2.Token {
	ch := make(chan string)
	randState := fmt.Sprintf("st%d", time.Now().UnixNano())
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/favicon.ico" {
			http.Error(rw, "", 404)
			return
		}
		if req.FormValue("state") != randState {
			log.Printf("State doesn't match: req = %#v", req)
			http.Error(rw, "", 500)
			return
		}
		if code := req.FormValue("code"); code != "" {
			fmt.Fprintf(rw, "<h1>Success</h1>Authorized.")
			rw.(http.Flusher).Flush()
			ch <- code
			return
		}
		log.Printf("no code")
		http.Error(rw, "", 500)
	}))
	defer ts.Close()

	config.RedirectURL = ts.URL
	authURL := config.AuthCodeURL(randState)
	go openURL(authURL)
	log.Printf("Authorize this app at: %s", authURL)
	code := <-ch
	log.Printf("Got code: %s", code)

	token, err := config.Exchange(ctx, code)
	if err != nil {
		log.Fatalf("Token exchange error: %v", err)
	}
	return token
}

func osUserCacheDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Caches")
	case "linux", "freebsd":
		return filepath.Join(os.Getenv("HOME"), ".cache")
	}
	log.Printf("TODO: osUserCacheDir on GOOS %q", runtime.GOOS)
	return "."
}

func openURL(url string) {
	try := []string{"xdg-open", "google-chrome", "open"}
	for _, bin := range try {
		err := exec.Command(bin, url).Run()
		if err == nil {
			return
		}
	}
	log.Printf("Error opening URL in browser.")
}

func findPlaylist(service *youtube.Service, title string) []string {
	playlists := youtube.NewPlaylistsService(service)
	playListsCall := playlists.List("snippet")
	playListsCall.Mine(true)
	playlistsResult, err := playListsCall.Do()
	if err != nil {
		log.Fatalf("Error listing playlists: %v", err)
	}

	for _, item := range playlistsResult.Items { // search of playlist
		mlib.DebugDump(item)
		if item.Snippet.Title == title {
			playlistsidslice = append(playlistsidslice, item.Id)
			return playlistsidslice
		}
		playlistsidslice = append(playlistsidslice, item.Id)
	}
	return playlistsidslice
}

func addToPlaylist(service *youtube.Service, videoID string, playlistIdslice []string) {
	items := youtube.NewPlaylistItemsService(service)
	fmt.Println(playlistIdslice[0])
	fmt.Println(videoID)
	itemInsertCall := items.Insert("snippet", &youtube.PlaylistItem{
		Snippet: &youtube.PlaylistItemSnippet{
			PlaylistId: playlistIdslice[0],
			ResourceId: &youtube.ResourceId{
				Kind:    "youtube#video",
				VideoId: videoID,
			},
		},
	})
	_, err := itemInsertCall.Do()
	if err != nil {
		log.Fatalf("Error adding video to playlist: %v", err)
	}
}

func createPlaylist(service *youtube.Service, title string) []string {
	playlists := youtube.NewPlaylistsService(service)

	playlist := youtube.Playlist{
		Snippet: &youtube.PlaylistSnippet{
			Title: title,
		},
		Status: &youtube.PlaylistStatus{
			PrivacyStatus: *privacy,
		},
	}

	playListsCall := playlists.Insert("snippet,status", &playlist)
	playlistsResult, err := playListsCall.Do()
	if err != nil {
		log.Fatalf("Error inserting playlist: %v", err)
	}
	mlib.DebugDump(playlistsResult)

	playlistsidslice = append(playlistsidslice, playlistsResult.Id)
	return playlistsidslice
}
