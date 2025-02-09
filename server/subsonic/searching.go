package subsonic

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/deluan/sanitize"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server/public"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/utils"
)

type searchParams struct {
	query        string
	artistCount  int
	artistOffset int
	albumCount   int
	albumOffset  int
	songCount    int
	songOffset   int
}

func (api *Router) getParams(r *http.Request) (*searchParams, error) {
	var err error
	sp := &searchParams{}
	sp.query, err = requiredParamString(r, "query")
	if err != nil {
		return nil, err
	}
	sp.artistCount = utils.ParamInt(r, "artistCount", 20)
	sp.artistOffset = utils.ParamInt(r, "artistOffset", 0)
	sp.albumCount = utils.ParamInt(r, "albumCount", 20)
	sp.albumOffset = utils.ParamInt(r, "albumOffset", 0)
	sp.songCount = utils.ParamInt(r, "songCount", 20)
	sp.songOffset = utils.ParamInt(r, "songOffset", 0)
	return sp, nil
}

type searchFunc[T any] func(q string, offset int, size int) (T, error)

func doSearch[T any](ctx context.Context, wg *sync.WaitGroup, s searchFunc[T], q string, offset, size int, result *T) {
	defer wg.Done()
	if size == 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		typ := strings.TrimPrefix(reflect.TypeOf(*result).String(), "model.")
		var err error
		start := time.Now()
		*result, err = s(q, offset, size)
		if err != nil {
			log.Error(ctx, "Error searching "+typ, "query", q, err)
		} else {
			log.Trace(ctx, "Search for "+typ+" completed", "query", q, "elapsed", time.Since(start))
		}
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (api *Router) searchAll(ctx context.Context, sp *searchParams) (mediaFiles model.MediaFiles, albums model.Albums, artists model.Artists) {
	start := time.Now()
	q := sanitize.Accents(strings.ToLower(strings.TrimSuffix(sp.query, "*")))
	wg := &sync.WaitGroup{}
	wg.Add(3)
	go doSearch(ctx, wg, api.ds.MediaFile(ctx).Search, q, sp.songOffset, sp.songCount, &mediaFiles)
	go doSearch(ctx, wg, api.ds.Album(ctx).Search, q, sp.albumOffset, sp.albumCount, &albums)
	go doSearch(ctx, wg, api.ds.Artist(ctx).Search, q, sp.artistOffset, sp.artistCount, &artists)
	wg.Wait()

	if ctx.Err() == nil {
		log.Debug(ctx, fmt.Sprintf("Search resulted in %d songs, %d albums and %d artists",
			len(mediaFiles), len(albums), len(artists)), "query", sp.query, "elapsedTime", time.Since(start))
	} else {
		log.Warn(ctx, "Search was interrupted", ctx.Err(), "query", sp.query, "elapsedTime", time.Since(start))
	}
	return mediaFiles, albums, artists
}

func (api *Router) Search2(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	sp, err := api.getParams(r)
	if err != nil {
		return nil, err
	}
	mfs, als, as := api.searchAll(ctx, sp)

	response := newResponse()
	searchResult2 := &responses.SearchResult2{}
	searchResult2.Artist = make([]responses.Artist, len(as))
	for i, artist := range as {
		artist := artist
		searchResult2.Artist[i] = responses.Artist{
			Id:             artist.ID,
			Name:           artist.Name,
			AlbumCount:     artist.AlbumCount,
			UserRating:     artist.Rating,
			CoverArt:       artist.CoverArtID().String(),
			ArtistImageUrl: public.ImageURL(r, artist.CoverArtID(), 600),
		}
		if artist.Starred {
			searchResult2.Artist[i].Starred = &as[i].StarredAt
		}
	}
	searchResult2.Album = childrenFromAlbums(ctx, als)
	searchResult2.Song = childrenFromMediaFiles(ctx, mfs)
	response.SearchResult2 = searchResult2
	return response, nil
}

func (api *Router) Search3(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	sp, err := api.getParams(r)
	if err != nil {
		return nil, err
	}
	mfs, als, as := api.searchAll(ctx, sp)

	response := newResponse()
	searchResult3 := &responses.SearchResult3{}
	searchResult3.Artist = make([]responses.ArtistID3, len(as))
	for i, artist := range as {
		searchResult3.Artist[i] = toArtistID3(r, artist)
	}
	searchResult3.Album = childrenFromAlbums(ctx, als)
	searchResult3.Song = childrenFromMediaFiles(ctx, mfs)
	response.SearchResult3 = searchResult3
	return response, nil
}
