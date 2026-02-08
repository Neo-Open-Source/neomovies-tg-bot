package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"handler/internal/neomovies"
	"handler/internal/storage"
	"handler/internal/tg"
)

type update struct {
	UpdateID      int             `json:"update_id"`
	InlineQuery   *inlineQuery    `json:"inline_query"`
	ChosenInline  *chosenInline   `json:"chosen_inline_result"`
	CallbackQuery *callbackQuery  `json:"callback_query"`
	Message       *message        `json:"message"`
	MyChatMember  json.RawMessage `json:"my_chat_member"`
}

type user struct {
	ID int64 `json:"id"`
}

type inlineQuery struct {
	ID    string `json:"id"`
	From  user   `json:"from"`
	Query string `json:"query"`
}

type chosenInline struct {
	ResultID        string `json:"result_id"`
	From            user   `json:"from"`
	Query           string `json:"query"`
	InlineMessageID string `json:"inline_message_id"`
}

type callbackQuery struct {
	ID              string   `json:"id"`
	From            user     `json:"from"`
	Data            string   `json:"data"`
	Message         *message `json:"message"`
	InlineMessageID string   `json:"inline_message_id"`
}

type autoSeriesState struct {
	KPID      int
	StartedAt time.Time
}

var autoSeriesMu sync.Mutex
var autoSeriesByChat = map[int64]autoSeriesState{}

type chat struct {
	ID int64 `json:"id"`
}

type message struct {
	MessageID            int      `json:"message_id"`
	Chat                 chat     `json:"chat"`
	Text                 string   `json:"text"`
	Caption              string   `json:"caption"`
	From                 *user    `json:"from"`
	ReplyToMessage       *message `json:"reply_to_message"`
	ForwardFromChat      *chat    `json:"forward_from_chat"`
	ForwardFromMessageID int      `json:"forward_from_message_id"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("webhook request: method=%s path=%s", r.Method, r.URL.Path)
	if r.URL.Path == "/api/library" || r.URL.Path == "/api/library/item" {
		libraryHandler(w, r)
		return
	}
	if r.URL.Path == "/api/player" {
		playerHandler(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var upd update
	if err := json.Unmarshal(body, &upd); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("BOT_TOKEN is required"))
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	bot := tg.NewClient(token)
	movies := neomovies.NewClient(apiBase)
	db, _ := storage.NewMongo(ctx, os.Getenv("MONGODB_URI"))

	switch {
	case upd.InlineQuery != nil:
		handleInlineQuery(ctx, w, bot, movies, db, upd.InlineQuery)
		return
	case upd.ChosenInline != nil:
		handleChosenInline(ctx, w, bot, movies, db, upd.ChosenInline)
		return
	case upd.CallbackQuery != nil:
		handleCallback(ctx, w, bot, movies, db, upd.CallbackQuery)
		return
	case upd.Message != nil:
		handleMessage(ctx, w, bot, movies, db, upd.Message)
		return
	default:
		w.WriteHeader(http.StatusOK)
		return
	}
}

type libraryItem struct {
	KPID          int      `json:"kp_id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	PosterURL     string   `json:"poster_url"`
	Rating        float64  `json:"rating"`
	Overview      string   `json:"overview"`
	Genres        []string `json:"genres,omitempty"`
	Voice         string   `json:"voice,omitempty"`
	Quality       string   `json:"quality,omitempty"`
	Seasons       []librarySeason `json:"seasons,omitempty"`
	SeasonsCount  int      `json:"seasons_count,omitempty"`
	EpisodesCount int      `json:"episodes_count,omitempty"`
	Voices        []string `json:"voices,omitempty"`
}

type librarySeason struct {
	Number   int             `json:"number"`
	Episodes []libraryEpisode `json:"episodes,omitempty"`
}

type libraryEpisode struct {
	Number  int    `json:"number"`
	Voice   string `json:"voice,omitempty"`
	Quality string `json:"quality,omitempty"`
	Variants []libraryEpisodeVariant `json:"variants,omitempty"`
}

type libraryEpisodeVariant struct {
	Voice   string `json:"voice,omitempty"`
	Quality string `json:"quality,omitempty"`
}

func libraryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	movies := neomovies.NewClient(apiBase)
	db, _ := storage.NewMongo(ctx, os.Getenv("MONGODB_URI"))
	if db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/api/library/item" {
		kpID, _ := strconv.Atoi(r.URL.Query().Get("kp_id"))
		if kpID <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out, err := buildLibraryItem(ctx, movies, item)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeJSON(w, out)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := db.ListRecent(ctx, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	out := make([]libraryItem, 0, len(items))
	for i := range items {
		li, err := buildLibraryItem(ctx, movies, &items[i])
		if err != nil {
			continue
		}
		out = append(out, li)
	}
	writeJSON(w, out)
}

func buildLibraryItem(ctx context.Context, movies *neomovies.Client, wItem *storage.WatchItem) (libraryItem, error) {
	info, err := movies.GetMovieByKPID(ctx, wItem.KPID)
	if err != nil {
		return libraryItem{}, err
	}
	if info == nil {
		return libraryItem{}, fmt.Errorf("not found")
	}

	// Fallback to search by kp_id if API returned empty fields
	if info.Title == "" && info.NameRu == "" && info.Name == "" && info.NameOriginal == "" ||
		(info.Overview == "" && info.Description == "" && info.ShortDescription == "") {
		if sr, _ := movies.SearchMovies(ctx, strconv.Itoa(wItem.KPID), 1); sr != nil {
			for _, m := range sr.Results {
				matchID := m.ExternalIDs.KP
				if matchID == 0 {
					matchID = m.KinopoiskID
				}
				if matchID != wItem.KPID {
					continue
				}
				if info.Title == "" && info.NameRu == "" && info.Name == "" && info.NameOriginal == "" {
					info.Title = m.Title
					info.NameRu = m.NameRu
					info.Name = m.Name
					info.NameOriginal = m.NameOriginal
				}
				if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
					info.Overview = m.Overview
					info.Description = m.Description
					info.ShortDescription = m.ShortDescription
				}
				if len(info.Genres) == 0 && len(m.Genres) > 0 {
					info.Genres = m.Genres
				}
				if info.Rating == 0 && m.Rating > 0 {
					info.Rating = m.Rating
				}
				if info.RatingKinopoisk == 0 && m.RatingKinopoisk > 0 {
					info.RatingKinopoisk = m.RatingKinopoisk
				}
				if info.VoteAverage == 0 && m.VoteAverage > 0 {
					info.VoteAverage = m.VoteAverage
				}
				if info.Year == "" && m.Year != "" {
					info.Year = m.Year
				}
				if info.ReleaseDate == "" && m.ReleaseDate != "" {
					info.ReleaseDate = m.ReleaseDate
				}
				if info.ExternalIDs.KP == 0 && m.ExternalIDs.KP != 0 {
					info.ExternalIDs.KP = m.ExternalIDs.KP
				}
				if info.ExternalIDs.IMDB == "" && m.ExternalIDs.IMDB != "" {
					info.ExternalIDs.IMDB = m.ExternalIDs.IMDB
				}
				if info.PosterPath == "" && m.PosterPath != "" {
					info.PosterPath = m.PosterPath
				}
				if info.PosterURLPreview == "" && m.PosterURLPreview != "" {
					info.PosterURLPreview = m.PosterURLPreview
				}
				if info.PosterURL == "" && m.PosterURL != "" {
					info.PosterURL = m.PosterURL
				}
				break
			}
		}
	}

	title := strings.TrimSpace(firstNonEmpty(info.Title, info.NameRu, info.Name, info.NameOriginal, wItem.Title))
	if title == "" {
		title = fmt.Sprintf("kp_%d", wItem.KPID)
	}
	poster := firstNonEmpty(info.PosterPath, info.PosterURLPreview, info.PosterURL)
	posterURL := movies.ImageURL(poster, "kp", wItem.KPID)
	if posterURL == "" {
		apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
		if apiBase == "" {
			apiBase = "https://api.neomovies.ru"
		}
		posterURL = fmt.Sprintf("%s/api/v1/images/kp/%d", apiBase, wItem.KPID)
	}
	rating := info.Rating
	if rating == 0 {
		rating = info.RatingKinopoisk
	}
	if rating == 0 {
		rating = info.VoteAverage
	}
	overview := strings.TrimSpace(firstNonEmpty(info.Overview, info.Description, info.ShortDescription))

	genres := make([]string, 0, len(info.Genres))
	for _, g := range info.Genres {
		name := strings.TrimSpace(g.Name)
		if name != "" {
			genres = append(genres, name)
		}
	}

	seasonsCount := 0
	episodesCount := 0
	if wItem.Type == "series" {
		seasonsCount = len(wItem.Seasons)
		for _, s := range wItem.Seasons {
			episodesCount += len(s.Episodes)
		}
	}

	seasons := []librarySeason{}
	if wItem.Type == "series" && len(wItem.Seasons) > 0 {
		seasons = make([]librarySeason, 0, len(wItem.Seasons))
		for _, s := range wItem.Seasons {
			ls := librarySeason{Number: s.Number}
			if len(s.Episodes) > 0 {
				ls.Episodes = make([]libraryEpisode, 0, len(s.Episodes))
				for _, ep := range s.Episodes {
					var variants []libraryEpisodeVariant
					if len(ep.Variants) > 0 {
						variants = make([]libraryEpisodeVariant, 0, len(ep.Variants))
						for _, v := range ep.Variants {
							variants = append(variants, libraryEpisodeVariant{
								Voice:   strings.TrimSpace(v.Voice),
								Quality: strings.TrimSpace(v.Quality),
							})
						}
					}
					ls.Episodes = append(ls.Episodes, libraryEpisode{
						Number:   ep.Number,
						Voice:    strings.TrimSpace(ep.Voice),
						Quality:  strings.TrimSpace(ep.Quality),
						Variants: variants,
					})
				}
			}
			seasons = append(seasons, ls)
		}
	}

	voices := []string{}
	imdb := strings.TrimSpace(info.ExternalIDs.IMDB)
	if imdb != "" {
		typeParam := "movie"
		if wItem.Type == "series" {
			typeParam = "tv"
		}
		trs, _ := movies.GetTorrentsByIMDB(ctx, imdb, typeParam)
		voices = neomovies.UniqueVoices(trs)
	}

	return libraryItem{
		KPID:          wItem.KPID,
		Type:          wItem.Type,
		Title:         title,
		PosterURL:     posterURL,
		Rating:        rating,
		Overview:      overview,
		Genres:        genres,
		Voice:         strings.TrimSpace(wItem.Voice),
		Quality:       strings.TrimSpace(wItem.Quality),
		Seasons:       seasons,
		SeasonsCount:  seasonsCount,
		EpisodesCount: episodesCount,
		Voices:        voices,
	}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func handleInlineQuery(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, q *inlineQuery) {
	query := strings.TrimSpace(q.Query)
	switch strings.ToLower(query) {
	case "#movies", "movies":
		query = "#movies"
	case "#tv", "tv", "series":
		query = "#tv"
	}
	iqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if query == "" {
		res, err := movies.GetPopular(iqCtx, 1)
		if err != nil {
			log.Printf("inline popular error: %v", err)
			_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
			w.WriteHeader(http.StatusOK)
			return
		}
		results := buildInlineResults(ctx, movies, db, res)
		if err := bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: results, CacheTime: 5, IsPersonal: true}); err != nil {
			log.Printf("inline popular answer error: %v (results=%d)", err, len(results))
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	res, err := movies.SearchMovies(iqCtx, query, 1)
	if err != nil {
		log.Printf("inline search error: %v (query=%q)", err, query)
		_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
		w.WriteHeader(http.StatusOK)
		return
	}

	results := buildInlineResults(ctx, movies, db, res)
	if err := bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: results, CacheTime: 5, IsPersonal: true}); err != nil {
		log.Printf("inline search answer error: %v (query=%q results=%d)", err, query, len(results))
	}
	w.WriteHeader(http.StatusOK)
}

func handleChosenInline(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, chosen *chosenInline) {
	kpID, err := strconv.Atoi(strings.TrimSpace(chosen.ResultID))
	if err != nil || kpID <= 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	payload, err := buildMoviePayload(ctx, movies, db, kpID)
	if err != nil {
		log.Printf("chosen inline payload error: %v (kp_id=%d)", err, kpID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if chosen.InlineMessageID != "" {
		if err := bot.EditMessageMedia(ctx, tg.EditMessageMediaRequest{
			InlineMessageID: chosen.InlineMessageID,
			Media: tg.InputMediaPhoto{
				Type:      "photo",
				Media:     payload.PhotoURL,
				Caption:   payload.Caption,
				ParseMode: "HTML",
			},
			ReplyMarkup: &payload.Keyboard,
		}); err != nil {
			log.Printf("chosen inline editMessageMedia error: %v (kp_id=%d)", err, kpID)
		}
	} else {
		if err := bot.SendPhoto(ctx, tg.SendPhotoRequest{
			ChatID:      chosen.From.ID,
			Photo:       payload.PhotoURL,
			Caption:     payload.Caption,
			ParseMode:   "HTML",
			ReplyMarkup: &payload.Keyboard,
		}); err != nil {
			log.Printf("chosen inline sendPhoto error: %v (kp_id=%d)", err, kpID)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func buildInlineResults(ctx context.Context, movies *neomovies.Client, db *storage.Mongo, res *neomovies.SearchResponse) []tg.InlineQueryResult {
	results := make([]tg.InlineQueryResult, 0, 10)
	for i, m := range res.Results {
		if i >= 10 {
			break
		}

		kpID := m.ExternalIDs.KP
		if kpID == 0 {
			kpID = m.KinopoiskID
		}
		if kpID == 0 {
			continue
		}

		inlineCache.Set(kpID, inlineMovieData{
			KPID:             kpID,
			Title:            firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal),
			Year:             m.Year,
			Overview:         m.Overview,
			Description:      m.Description,
			ShortDescription: m.ShortDescription,
			PosterPath:       m.PosterPath,
			PosterURL:        m.PosterURL,
			PosterURLPreview: m.PosterURLPreview,
			Rating:           m.Rating,
			RatingKinopoisk:  m.RatingKinopoisk,
			VoteAverage:      m.VoteAverage,
			Genres:           m.Genres,
		}, 30*time.Minute)

		title := firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal)
		year := ""
		if m.Year != "" {
			year = m.Year
		} else if m.ReleaseDate != "" {
			year = m.ReleaseDate[0:4]
		}
		displayTitle := title
		if year != "" {
			displayTitle = fmt.Sprintf("%s (%s)", title, year)
		}

		rating := m.Rating
		if rating == 0 {
			rating = m.RatingKinopoisk
		}
		if rating == 0 {
			rating = m.VoteAverage
		}

		desc := strings.TrimSpace(firstNonEmpty(m.Overview, m.Description, m.ShortDescription))
		if len([]rune(desc)) > 900 {
			desc = string([]rune(desc)[:900]) + "…"
		}

		poster := firstNonEmpty(m.PosterPath, m.PosterURLPreview, m.PosterURL)
		thumbURL := movies.ImageURL(poster, "kp_small", kpID)

		caption := html.EscapeString(displayTitle)
		if rating > 0 {
			caption = fmt.Sprintf("%s\nКинопоиск: %.1f", caption, rating)
		}
		if desc != "" {
			caption = caption + "\n\n" + html.EscapeString(desc)
		}
		caption = truncateRunes(caption, 950)

		descLines := make([]string, 0, 2)
		if rating > 0 {
			descLines = append(descLines, fmt.Sprintf("Кинопоиск: %.1f", rating))
		}
		if len(m.Genres) > 0 {
			genres := make([]string, 0, 3)
			for _, g := range m.Genres {
				name := strings.TrimSpace(g.Name)
				if name != "" {
					genres = append(genres, name)
				}
				if len(genres) >= 3 {
					break
				}
			}
			if len(genres) > 0 {
				descLines = append(descLines, strings.Join(genres, ", "))
			}
		}
		description := truncateRunes(strings.Join(descLines, " • "), 180)

		messageText := fmt.Sprintf("/get %d", kpID)

		result := tg.InlineQueryResultArticle{
			Type:  "article",
			ID:    strconv.FormatInt(int64(kpID), 10),
			Title: displayTitle,
			InputMessageContent: tg.InputTextMessageContent{
				MessageText: messageText,
				ParseMode:   "HTML",
			},
			Description: description,
		}
		if thumbURL != "" {
			result.ThumbURL = thumbURL
			result.ThumbWidth = 80
			result.ThumbHeight = 120
		}
		results = append(results, result)
	}
	return results
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func handleCallback(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, cq *callbackQuery) {
	data := strings.TrimSpace(cq.Data)
	if data == "close" {
		if cq.Message != nil {
			_ = bot.DeleteMessage(ctx, cq.Message.Chat.ID, cq.Message.MessageID)
		} else if cq.InlineMessageID != "" {
			_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{InlineMessageID: cq.InlineMessageID, ReplyMarkup: &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{}}})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if data == "menu:new" || data == "menu:movies" || data == "menu:series" {
		if cq.Message != nil {
			query := ""
			text := "Открой поиск и набери название — я покажу карточки.\n\nПодсказка: можно нажать кнопку “Поиск” в меню."
			if data == "menu:movies" {
				query = "#movies"
				text = "Топ фильмов. Нажми кнопку ниже."
			} else if data == "menu:series" {
				query = "#tv"
				text = "Топ сериалов. Нажми кнопку ниже."
			}
			var kb *tg.InlineKeyboardMarkup
			if query != "" {
				markup := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
					{{Text: "Показать", SwitchInlineQueryCurrentChat: tg.StrPtr(query)}},
				})
				kb = &markup
			}
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: cq.Message.Chat.ID, Text: text, ReplyMarkup: kb})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "watch:") {
		kpStr := strings.TrimPrefix(data, "watch:")
		kpID, _ := strconv.Atoi(kpStr)
		if kpID <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}

		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "Нет в Telegram")
			w.WriteHeader(http.StatusOK)
			return
		}

		if cq.Message != nil {
			if item.Type == "movie" {
				messageIDs := movieMessageIDs(item)
				var lastCopied int
				failed := []int{}
				for _, mid := range messageIDs {
					copiedID, err := bot.CopyMessage(ctx, cq.Message.Chat.ID, item.StorageChatID, mid)
					if err == nil && copiedID > 0 {
						lastCopied = copiedID
						log.Printf("copy movie part ok kp_id=%d mid=%d new_id=%d", item.KPID, mid, copiedID)
					} else {
						log.Printf("copy movie part error kp_id=%d mid=%d err=%v", item.KPID, mid, err)
						failed = append(failed, mid)
					}
					time.Sleep(250 * time.Millisecond)
				}
				if lastCopied > 0 {
					closeKB := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
						{{Text: "Закрыть", CallbackData: "close"}},
					})
					_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
						ChatID:      cq.Message.Chat.ID,
						MessageID:   lastCopied,
						ReplyMarkup: &closeKB,
					})
				}
				if len(failed) > 0 {
					_ = bot.SendMessage(ctx, tg.SendMessageRequest{
						ChatID: cq.Message.Chat.ID,
						Text:   fmt.Sprintf("Не удалось скопировать части: %s", joinMessageIDs(failed)),
					})
				}
			} else if item.Type == "series" {
				title := strings.TrimSpace(item.Title)
				if title == "" {
					title = fmt.Sprintf("kp_%d", item.KPID)
				}
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: cq.Message.Chat.ID, Text: title, ReplyMarkup: item.SeriesKeyboard()})
			}
		}

		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "season:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		if kpID <= 0 || seasonNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if cq.Message != nil {
			baseVoice := strings.TrimSpace(item.Voice)
			if baseVoice == "" {
				season := findSeason(item, seasonNum)
				if season != nil {
					baseVoice = mostCommonEpisodeValue(season, func(ep storage.Episode) string { return ep.Voice })
				}
			}
			text := buildSeasonHeader(item, seasonNum)
			_ = bot.EditMessageText(ctx, tg.EditMessageTextRequest{
				ChatID:      cq.Message.Chat.ID,
				MessageID:   cq.Message.MessageID,
				Text:        text,
				ReplyMarkup: item.SeasonKeyboard(seasonNum, 1, baseVoice),
			})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "seasonpage:") {
		parts := strings.Split(data, ":")
		if len(parts) != 4 && len(parts) != 5 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		pageNum, _ := strconv.Atoi(parts[3])
		voice := ""
		if len(parts) == 5 && parts[4] != "" {
			if v, err := url.QueryUnescape(parts[4]); err == nil {
				voice = v
			}
		}
		if kpID <= 0 || seasonNum <= 0 || pageNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if cq.Message != nil {
			_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
				ChatID:      cq.Message.Chat.ID,
				MessageID:   cq.Message.MessageID,
				ReplyMarkup: item.SeasonKeyboard(seasonNum, pageNum, voice),
			})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "seasonvoice:") {
		parts := strings.Split(data, ":")
		if len(parts) != 4 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		voice := strings.TrimSpace(parts[3])
		if voice == "all" {
			voice = ""
		} else {
			if v, err := url.QueryUnescape(voice); err == nil {
				voice = v
			}
		}
		if kpID <= 0 || seasonNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if cq.Message != nil {
			_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
				ChatID:      cq.Message.Chat.ID,
				MessageID:   cq.Message.MessageID,
				ReplyMarkup: item.SeasonKeyboard(seasonNum, 1, voice),
			})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "ep:") {
		parts := strings.Split(data, ":")
		if len(parts) != 4 && len(parts) != 5 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		voice := ""
		if len(parts) == 5 && parts[4] != "" {
			if parts[4] == "select" {
				voice = ""
			} else if v, err := url.QueryUnescape(parts[4]); err == nil {
				voice = v
			}
		}
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		var chatID int64
		if cq.Message != nil {
			chatID = cq.Message.Chat.ID
		}
		if chatID == 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := sendEpisodeWithNav(ctx, bot, item, chatID, seasonNum, epNum, voice); err != nil {
			log.Printf("episode send error: %v (kp_id=%d s=%d e=%d)", err, kpID, seasonNum, epNum)
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "epnav:") {
		parts := strings.Split(data, ":")
		if len(parts) != 5 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		dir, _ := strconv.Atoi(parts[4])
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 || (dir != -1 && dir != 1) {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		chatID := int64(0)
		msgID := 0
		if cq.Message != nil {
			chatID = cq.Message.Chat.ID
			msgID = cq.Message.MessageID
		}
		if chatID == 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		nextEp := epNum + dir
		if err := sendEpisodeWithNav(ctx, bot, item, chatID, seasonNum, nextEp, ""); err != nil {
			log.Printf("episode nav error: %v (kp_id=%d s=%d e=%d)", err, kpID, seasonNum, nextEp)
		} else if msgID != 0 {
			_ = bot.DeleteMessage(ctx, chatID, msgID)
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "epv:") {
		parts := strings.Split(data, ":")
		if len(parts) != 5 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		voice, _ := url.QueryUnescape(parts[4])
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		var chatID int64
		if cq.Message != nil {
			chatID = cq.Message.Chat.ID
		}
		if chatID == 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := sendEpisodeWithNav(ctx, bot, item, chatID, seasonNum, epNum, voice); err != nil {
			log.Printf("episode voice send error: %v (kp_id=%d s=%d e=%d)", err, kpID, seasonNum, epNum)
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}

	_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
	w.WriteHeader(http.StatusOK)
}

func handleMessage(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, msg *message) {
	text := strings.TrimSpace(msg.Text)
	log.Printf("message received chat_id=%d text=%q", msg.Chat.ID, text)
	if strings.HasPrefix(text, "/start") {
		log.Printf("/start from chat_id=%d", msg.Chat.ID)
		parts := strings.Fields(text)
		if len(parts) > 1 {
			payload := strings.TrimSpace(parts[1])
			if strings.HasPrefix(payload, "get_") {
				idStr := strings.TrimPrefix(payload, "get_")
				if kpID, _ := strconv.Atoi(idStr); kpID > 0 {
					if err := sendMovieCard(ctx, bot, movies, db, msg.Chat.ID, kpID); err != nil {
						log.Printf("start get error: %v (kp_id=%d)", err, kpID)
					}
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
		kb := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
			{{Text: "Фильмы", CallbackData: "menu:movies"}, {Text: "Сериалы", CallbackData: "menu:series"}},
			{{Text: "Поиск", SwitchInlineQueryCurrentChat: tg.StrPtr("")}},
			{{Text: "Кинотека в боте(Сайт)", URL: "https://tg.neomovies.ru/"}}, {{Text: "Кинотека в боте(Канал)", URL: "https://t.me/neomovies_tg"}},
			{{Text: "Закрыть", CallbackData: "close"}},
		})
		if err := bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Это библиотека кино и сериалов с быстрым поиском.\n\nНажми “Поиск” и введи название — я покажу карточки.\n\nЕсли просто написать @neomovies_tg_bot без текста — покажу популярное.", ReplyMarkup: &kb}); err != nil {
			log.Printf("/start sendMessage error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/get ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := sendMovieCard(ctx, bot, movies, db, msg.Chat.ID, kpID); err != nil {
			log.Printf("public /get error: %v (kp_id=%d)", err, kpID)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if handled := handleAutoEpisode(ctx, bot, db, msg); handled {
		w.WriteHeader(http.StatusOK)
		return
	}

	if text == "/help" || text == "help" {
		adminChatIDStr := strings.TrimSpace(os.Getenv("ADMIN_CHAT_ID"))
		if adminChatIDStr == "" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID не задан. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
		if err != nil || adminChatID == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID неверный. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		if msg.Chat.ID != adminChatID {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Нет доступа. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "/help\n\n/addmovie <kp_id> <voice> <quality> <storage_chat_id> <storage_message_id[,storage_message_id...]>\n/addmovie <kp_id> <voice> <quality>   (reply to forwarded channel post)\n/addmoviepart <kp_id>   (reply to forwarded channel post, append part)\n\n/addseries <kp_id> <title>\n\n/addepisode <kp_id> <season> <episode> <voice> <quality> <storage_chat_id> <storage_message_id>\n/addepisode <kp_id> <season> <episode> <voice> <quality>   (reply to forwarded channel post)\n\n/autoaddepisodes <kp_id>\n/autostop\n\n/delepisode <kp_id> <season> <episode>\n/delseason <kp_id> <season>\n\n/getinfo <kp_id>\n/del <kp_id>\n/list [limit]"})
		w.WriteHeader(http.StatusOK)
		return
	}

	adminChatIDStr := strings.TrimSpace(os.Getenv("ADMIN_CHAT_ID"))
	if adminChatIDStr == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
	if err != nil || adminChatID == 0 {
		if text == "/help" || text == "help" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID неверный. Твой chat_id=%d", msg.Chat.ID)})
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if msg.Chat.ID != adminChatID {
		if text == "/help" || text == "help" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Нет доступа. Твой chat_id=%d", msg.Chat.ID)})
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if strings.HasPrefix(text, "/addmovie ") {
		parts := strings.Fields(text)
		if len(parts) != 4 && len(parts) != 6 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addmovie <kp_id> <voice> <quality> <storage_chat_id> <storage_message_id[,storage_message_id...]> OR reply to forwarded post: /addmovie <kp_id> <voice> <quality>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		voice := strings.TrimSpace(parts[2])
		quality := strings.TrimSpace(parts[3])
		var storageChatID int64
		var storageMsgIDs []int
		if len(parts) == 6 {
			storageChatID, _ = strconv.ParseInt(parts[4], 10, 64)
			storageMsgIDs = parseMessageIDList(parts[5])
		} else {
			if msg.ReplyToMessage == nil || msg.ReplyToMessage.ForwardFromChat == nil || msg.ReplyToMessage.ForwardFromMessageID == 0 {
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Reply to a forwarded post from the storage channel."})
				w.WriteHeader(http.StatusOK)
				return
			}
			storageChatID = msg.ReplyToMessage.ForwardFromChat.ID
			storageMsgIDs = []int{msg.ReplyToMessage.ForwardFromMessageID}
		}
		if kpID <= 0 || voice == "" || quality == "" || storageChatID == 0 || len(storageMsgIDs) == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertWatchMovie(ctx, kpID, voice, quality, storageChatID, storageMsgIDs)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/autoaddepisodes ") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /autoaddepisodes <kp_id>"} )
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		autoSeriesMu.Lock()
		autoSeriesByChat[msg.Chat.ID] = autoSeriesState{KPID: kpID, StartedAt: time.Now()}
		autoSeriesMu.Unlock()
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("OK. Автодобавление включено для kp_id=%d. Пересылай посты с видео.", kpID)})
		w.WriteHeader(http.StatusOK)
		return
	}
	if text == "/autostop" || text == "/autoaddepisodes stop" {
		autoSeriesMu.Lock()
		delete(autoSeriesByChat, msg.Chat.ID)
		autoSeriesMu.Unlock()
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK. Автодобавление выключено."})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/addmoviepart ") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addmoviepart <kp_id> (reply to forwarded post)"} )
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		if msg.ReplyToMessage == nil || msg.ReplyToMessage.ForwardFromChat == nil || msg.ReplyToMessage.ForwardFromMessageID == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Reply to a forwarded post from the storage channel."})
			w.WriteHeader(http.StatusOK)
			return
		}
		storageChatID := msg.ReplyToMessage.ForwardFromChat.ID
		storageMsgID := msg.ReplyToMessage.ForwardFromMessageID
		if err := db.AppendMovieParts(ctx, kpID, storageChatID, []int{storageMsgID}); err != nil {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Error: %v", err)})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/addseries ") {
		rest := strings.TrimSpace(strings.TrimPrefix(text, "/addseries"))
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addseries <kp_id> <title>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(fields[0])
		title := strings.TrimSpace(rest[len(fields[0]):])
		if kpID <= 0 || title == "" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertWatchSeries(ctx, kpID, title)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/addepisode ") {
		parts := strings.Fields(text)
		if len(parts) != 6 && len(parts) != 8 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addepisode <kp_id> <season> <episode> <voice> <quality> <storage_chat_id> <storage_message_id> OR reply to forwarded post: /addepisode <kp_id> <season> <episode> <voice> <quality>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		voice := strings.TrimSpace(parts[4])
		quality := strings.TrimSpace(parts[5])
		var storageChatID int64
		var storageMsgID int
		if len(parts) == 8 {
			storageChatID, _ = strconv.ParseInt(parts[6], 10, 64)
			storageMsgID, _ = strconv.Atoi(parts[7])
		} else {
			if msg.ReplyToMessage == nil || msg.ReplyToMessage.ForwardFromChat == nil || msg.ReplyToMessage.ForwardFromMessageID == 0 {
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Reply to a forwarded post from the storage channel."})
				w.WriteHeader(http.StatusOK)
				return
			}
			storageChatID = msg.ReplyToMessage.ForwardFromChat.ID
			storageMsgID = msg.ReplyToMessage.ForwardFromMessageID
		}
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 || voice == "" || quality == "" || storageChatID == 0 || storageMsgID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertSeriesEpisode(ctx, kpID, seasonNum, epNum, voice, quality, storageChatID, storageMsgID)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/getinfo ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /get <kp_id>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Not found"})
			w.WriteHeader(http.StatusOK)
			return
		}
		ref := fmt.Sprintf("%d:%d", item.StorageChatID, item.StorageMessageID)
		if len(item.StorageMessageIDs) > 0 {
			ref = fmt.Sprintf("%d:%s", item.StorageChatID, joinMessageIDs(item.StorageMessageIDs))
		}
		textOut := fmt.Sprintf("kp_id=%d\ntype=%s\ntitle=%s\nmovie_ref=%s\nseasons=%d", item.KPID, item.Type, item.Title, ref, len(item.Seasons))
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: textOut})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/delepisode ") {
		parts := strings.Fields(text)
		if len(parts) < 4 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /delepisode <kp_id> <season> <episode>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.DeleteSeriesEpisode(ctx, kpID, seasonNum, epNum)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/delseason ") {
		parts := strings.Fields(text)
		if len(parts) < 3 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /delseason <kp_id> <season>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		if kpID <= 0 || seasonNum <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.DeleteSeason(ctx, kpID, seasonNum)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/del ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /del <kp_id>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.DeleteByKPID(ctx, kpID)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/list") {
		parts := strings.Fields(text)
		limit := 20
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				limit = n
			}
		}
		items, err := db.ListRecent(ctx, limit)
		if err != nil {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "DB not configured"})
			w.WriteHeader(http.StatusOK)
			return
		}
		if len(items) == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Empty"})
			w.WriteHeader(http.StatusOK)
			return
		}
		b := strings.Builder{}
		for _, it := range items {
			name := strings.TrimSpace(it.Title)
			if name == "" {
				name = fmt.Sprintf("kp_%d", it.KPID)
			}
			b.WriteString(fmt.Sprintf("%d %s %s\n", it.KPID, it.Type, name))
		}
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: strings.TrimSpace(b.String())})
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleAutoEpisode(ctx context.Context, bot *tg.Client, db *storage.Mongo, msg *message) bool {
	if db == nil {
		return false
	}
	autoSeriesMu.Lock()
	state, ok := autoSeriesByChat[msg.Chat.ID]
	autoSeriesMu.Unlock()
	if !ok {
		return false
	}

	if msg.ReplyToMessage != nil {
		// don't intercept replies
		return false
	}
	if msg.ForwardFromChat == nil || msg.ForwardFromMessageID == 0 {
		return false
	}

	caption := strings.TrimSpace(firstNonEmpty(msg.Caption, msg.Text))
	if caption == "" {
		return false
	}
	season, episode, voice, quality := parseEpisodeCaption(caption)
	if season <= 0 || episode <= 0 {
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Не удалось распознать сезон/серию из подписи."})
		return true
	}
	if voice == "" {
		voice = "Unknown"
	}
	if quality == "" {
		quality = "Unknown"
	}

	err := db.UpsertSeriesEpisode(ctx, state.KPID, season, episode, voice, quality, msg.ForwardFromChat.ID, msg.ForwardFromMessageID)
	if err != nil {
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Ошибка добавления: %v", err)})
		return true
	}
	_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("OK: S%dE%d, %s, %s", season, episode, voice, quality)})
	return true
}

func parseEpisodeCaption(caption string) (season int, episode int, voice string, quality string) {
	c := strings.TrimSpace(caption)
	reSeason := regexp.MustCompile(`(?i)сезон\s*(\d{1,2})`)
	reEpisode := regexp.MustCompile(`(?i)серия\s*(\d{1,3})`)
	if m := reSeason.FindStringSubmatch(c); len(m) == 2 {
		season, _ = strconv.Atoi(m[1])
	}
	if m := reEpisode.FindStringSubmatch(c); len(m) == 2 {
		episode, _ = strconv.Atoi(m[1])
	}

	reParens := regexp.MustCompile(`\(([^)]+)\)`)
	if m := reParens.FindStringSubmatch(c); len(m) == 2 {
		parts := strings.Split(m[1], ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.Contains(strings.ToLower(p), "p") && strings.ContainsAny(p, "0123456789") {
				quality = p
				continue
			}
			if voice == "" {
				voice = p
			}
		}
	}

	// Also try to detect quality outside parentheses
	if quality == "" {
		reQuality := regexp.MustCompile(`(?i)\b(\d{3,4}p)\b`)
		if m := reQuality.FindStringSubmatch(c); len(m) == 2 {
			quality = m[1]
		}
	}

	return
}

var iframeSrcRe = regexp.MustCompile(`(?i)src=\"([^\"]+)\"`)
var iframeDataSrcRe = regexp.MustCompile(`(?i)data-src=\"([^\"]+)\"`)

func playerHandler(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	idType := strings.TrimSpace(r.URL.Query().Get("idType"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if provider == "" || idType == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	u := fmt.Sprintf("%s/api/v1/players/%s/%s/%s", apiBase, url.PathEscape(provider), url.PathEscape(idType), url.PathEscape(id))

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	content := strings.TrimSpace(string(b))
	if strings.HasPrefix(content, "<") {
		if m := iframeSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		if m := iframeDataSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}

	http.Redirect(w, r, content, http.StatusFound)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

type moviePayload struct {
	PhotoURL string
	Caption  string
	Keyboard tg.InlineKeyboardMarkup
}

type inlineMovieData struct {
	KPID             int
	Title            string
	Year             string
	Overview         string
	Description      string
	ShortDescription string
	PosterPath       string
	PosterURL        string
	PosterURLPreview string
	Rating           float64
	RatingKinopoisk  float64
	VoteAverage      float64
	Genres           []neomovies.MovieGenre
}

type inlineCacheEntry struct {
	data    inlineMovieData
	expires time.Time
}

type inlineMovieCache struct {
	mu    sync.Mutex
	items map[int]inlineCacheEntry
}

func newInlineMovieCache() *inlineMovieCache {
	return &inlineMovieCache{items: map[int]inlineCacheEntry{}}
}

func (c *inlineMovieCache) Get(kpID int) (inlineMovieData, bool) {
	if kpID <= 0 {
		return inlineMovieData{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[kpID]
	if !ok {
		return inlineMovieData{}, false
	}
	if time.Now().After(entry.expires) {
		delete(c.items, kpID)
		return inlineMovieData{}, false
	}
	return entry.data, true
}

func (c *inlineMovieCache) Set(kpID int, data inlineMovieData, ttl time.Duration) {
	if kpID <= 0 {
		return
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	c.mu.Lock()
	c.items[kpID] = inlineCacheEntry{data: data, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
}

var inlineCache = newInlineMovieCache()

func buildMoviePayload(ctx context.Context, movies *neomovies.Client, db *storage.Mongo, kpID int) (*moviePayload, error) {
	if kpID <= 0 {
		return nil, fmt.Errorf("invalid kp_id")
	}

	log.Printf("movie payload: kp_id=%d", kpID)

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	info, err := movies.GetMovieByKPID(ctx, kpID)
	if err != nil || info == nil {
		log.Printf("movie payload: GetMovieByKPID failed kp_id=%d err=%v", kpID, err)
		return nil, fmt.Errorf("get movie failed: %v", err)
	}
	log.Printf("movie payload: GetMovieByKPID ok kp_id=%d title=%q nameRu=%q name=%q year=%q release=%q rating=%.2f kp=%.2f vote=%.2f poster=%q", kpID, info.Title, info.NameRu, info.Name, info.Year, info.ReleaseDate, info.Rating, info.RatingKinopoisk, info.VoteAverage, info.PosterPath)

	title := strings.TrimSpace(firstNonEmpty(info.Title, info.NameRu, info.Name, info.NameOriginal))
	if cached, ok := inlineCache.Get(kpID); ok {
		if title == "" {
			title = strings.TrimSpace(firstNonEmpty(cached.Title))
		}
		if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
			info.Overview = cached.Overview
			info.Description = cached.Description
			info.ShortDescription = cached.ShortDescription
		}
		if len(info.Genres) == 0 && len(cached.Genres) > 0 {
			info.Genres = cached.Genres
		}
		if info.Rating == 0 {
			info.Rating = cached.Rating
		}
		if info.RatingKinopoisk == 0 {
			info.RatingKinopoisk = cached.RatingKinopoisk
		}
		if info.VoteAverage == 0 {
			info.VoteAverage = cached.VoteAverage
		}
		if info.Year == "" {
			info.Year = cached.Year
		}
		if info.ReleaseDate == "" {
			info.ReleaseDate = cached.Year
		}
		if info.PosterPath == "" && cached.PosterPath != "" {
			info.PosterPath = cached.PosterPath
		}
		if info.PosterURL == "" && cached.PosterURL != "" {
			info.PosterURL = cached.PosterURL
		}
		if info.PosterURLPreview == "" && cached.PosterURLPreview != "" {
			info.PosterURLPreview = cached.PosterURLPreview
		}
	}
	if title == "" && db != nil {
		if item, _ := db.GetWatchItemByKPID(ctx, kpID); item != nil {
			title = strings.TrimSpace(item.Title)
		}
	}
	if title == "" || (info.Overview == "" && info.Description == "" && info.ShortDescription == "") {
		log.Printf("movie payload: fallback search kp_id=%d", kpID)
		if sr, _ := movies.SearchMovies(ctx, strconv.Itoa(kpID), 1); sr != nil {
			log.Printf("movie payload: search results kp_id=%d count=%d", kpID, len(sr.Results))
			for _, m := range sr.Results {
				matchID := m.ExternalIDs.KP
				if matchID == 0 {
					matchID = m.KinopoiskID
				}
				if matchID != kpID {
					continue
				}
				log.Printf("movie payload: search matched kp_id=%d title=%q nameRu=%q year=%q rating=%.2f kp=%.2f vote=%.2f", kpID, m.Title, m.NameRu, m.Year, m.Rating, m.RatingKinopoisk, m.VoteAverage)
				if title == "" {
					title = strings.TrimSpace(firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal))
				}
				if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
					info.Overview = m.Overview
					info.Description = m.Description
					info.ShortDescription = m.ShortDescription
				}
				if len(info.Genres) == 0 && len(m.Genres) > 0 {
					info.Genres = m.Genres
				}
				if info.Rating == 0 && m.Rating > 0 {
					info.Rating = m.Rating
				}
				if info.RatingKinopoisk == 0 && m.RatingKinopoisk > 0 {
					info.RatingKinopoisk = m.RatingKinopoisk
				}
				if info.VoteAverage == 0 && m.VoteAverage > 0 {
					info.VoteAverage = m.VoteAverage
				}
				if info.Year == "" && m.Year != "" {
					info.Year = m.Year
				}
				if info.ReleaseDate == "" && m.ReleaseDate != "" {
					info.ReleaseDate = m.ReleaseDate
				}
				break
			}
		}
	}

	if title == "" {
		title = fmt.Sprintf("kp_%d", kpID)
	}
	year := ""
	if info.Year != "" {
		year = info.Year
	} else if info.ReleaseDate != "" && len(info.ReleaseDate) >= 4 {
		year = info.ReleaseDate[0:4]
	}
	displayTitle := title
	if year != "" {
		displayTitle = fmt.Sprintf("%s (%s)", title, year)
	}

	rating := info.Rating
	if rating == 0 {
		rating = info.RatingKinopoisk
	}
	if rating == 0 {
		rating = info.VoteAverage
	}

	desc := strings.TrimSpace(firstNonEmpty(info.Overview, info.Description, info.ShortDescription))
	if len([]rune(desc)) > 900 {
		desc = string([]rune(desc)[:900]) + "…"
	}

	photoURL := fmt.Sprintf("%s/api/v1/images/kp/%d", apiBase, kpID)
	log.Printf("movie payload: kp_id=%d photoURL=%q poster=%q desc_len=%d", kpID, photoURL, info.PosterPath, len([]rune(desc)))

	keyboard := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
		{
			{Text: "Плеер 1 (Collaps)", URL: movies.PlayerRedirectURL("collaps", "kp", kpID)},
			{Text: "Плеер 2 (Lumex)", URL: movies.PlayerRedirectURL("lumex", "kp", kpID)},
		},
	})
	if db != nil {
		if watch, _ := db.GetWatchItemByKPID(ctx, kpID); watch != nil {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Смотреть в Telegram", CallbackData: fmt.Sprintf("watch:%d", kpID)}})
		}
	}
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})

	captionLines := []string{fmt.Sprintf("<b>%s</b>", html.EscapeString(displayTitle))}
	if rating > 0 {
		captionLines = append(captionLines, fmt.Sprintf("<b>Кинопоиск</b>: %.1f", rating))
	}
	if len(info.Genres) > 0 {
		genres := make([]string, 0, 4)
		for _, g := range info.Genres {
			name := strings.TrimSpace(g.Name)
			if name != "" {
				genres = append(genres, name)
			}
			if len(genres) >= 4 {
				break
			}
		}
		if len(genres) > 0 {
			captionLines = append(captionLines, fmt.Sprintf("<b>%s</b>", html.EscapeString(strings.Join(genres, ", "))))
		}
	}
	caption := strings.Join(captionLines, "\n")
	if desc != "" {
		caption = caption + "\n\n" + html.EscapeString("«"+desc+"»")
	}
	caption = truncateRunes(caption, 950)
	if strings.TrimSpace(caption) == "" {
		caption = html.EscapeString(fmt.Sprintf("kp_%d", kpID))
	}

	return &moviePayload{
		PhotoURL: photoURL,
		Caption:  caption,
		Keyboard: keyboard,
	}, nil
}

func sendMovieCard(ctx context.Context, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, chatID int64, kpID int) error {
	if kpID <= 0 {
		return fmt.Errorf("invalid kp_id")
	}

	payload, err := buildMoviePayload(ctx, movies, db, kpID)
	if err != nil {
		return err
	}
	if payload.PhotoURL != "" {
		if err := bot.SendPhoto(ctx, tg.SendPhotoRequest{
			ChatID:      chatID,
			Photo:       payload.PhotoURL,
			Caption:     payload.Caption,
			ParseMode:   "HTML",
			ReplyMarkup: &payload.Keyboard,
		}); err == nil {
			return nil
		}
	}

	return bot.SendMessage(ctx, tg.SendMessageRequest{
		ChatID:      chatID,
		Text:        payload.Caption,
		ParseMode:   "HTML",
		ReplyMarkup: &payload.Keyboard,
	})
}

func stripHTMLTags(s string) string {
	out := strings.Builder{}
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

func findSeason(item *storage.WatchItem, seasonNum int) *storage.Season {
	if item == nil {
		return nil
	}
	for i := range item.Seasons {
		if item.Seasons[i].Number == seasonNum {
			return &item.Seasons[i]
		}
	}
	return nil
}

func findEpisode(season *storage.Season, epNum int) (*storage.Episode, int) {
	if season == nil {
		return nil, -1
	}
	for i := range season.Episodes {
		if season.Episodes[i].Number == epNum {
			return &season.Episodes[i], i
		}
	}
	return nil, -1
}

func sendEpisodeWithNav(ctx context.Context, bot *tg.Client, item *storage.WatchItem, chatID int64, seasonNum int, epNum int, voice string) error {
	season := findSeason(item, seasonNum)
	if season == nil {
		return fmt.Errorf("season not found")
	}
	ep, idx := findEpisode(season, epNum)
	if ep == nil || idx == -1 {
		return fmt.Errorf("episode not found")
	}

	voice = strings.TrimSpace(voice)
	if len(ep.Variants) > 1 && voice == "" {
		rows := [][]tg.InlineKeyboardButton{}
		row := []tg.InlineKeyboardButton{}
		for _, v := range ep.Variants {
			vName := strings.TrimSpace(v.Voice)
			if vName == "" {
				vName = "Без названия"
			}
			btn := tg.InlineKeyboardButton{
				Text:         vName,
				CallbackData: fmt.Sprintf("epv:%d:%d:%d:%s", item.KPID, seasonNum, epNum, url.QueryEscape(vName)),
			}
			if len(row) == 2 {
				rows = append(rows, row)
				row = []tg.InlineKeyboardButton{}
			}
			row = append(row, btn)
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
		rows = append(rows, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})
		kb := tg.NewInlineKeyboardMarkup(rows)
		return bot.SendMessage(ctx, tg.SendMessageRequest{
			ChatID:      chatID,
			Text:        fmt.Sprintf("Выбери озвучку: S%dE%d", seasonNum, epNum),
			ReplyMarkup: &kb,
		})
	}

	storageChatID := ep.StorageChatID
	storageMsgID := ep.StorageMessageID
	if len(ep.Variants) > 0 && voice != "" {
		for _, v := range ep.Variants {
			if strings.EqualFold(strings.TrimSpace(v.Voice), voice) {
				storageChatID = v.StorageChatID
				storageMsgID = v.StorageMessageID
				break
			}
		}
	}

	copiedID, err := bot.CopyMessage(ctx, chatID, storageChatID, storageMsgID)
	if err != nil || copiedID <= 0 {
		return err
	}

	hasPrev := idx > 0
	hasNext := idx < len(season.Episodes)-1
	rows := make([][]tg.InlineKeyboardButton, 0, 2)
	if hasPrev || hasNext {
		nav := []tg.InlineKeyboardButton{}
		if hasPrev {
			nav = append(nav, tg.InlineKeyboardButton{Text: "<<<", CallbackData: fmt.Sprintf("epnav:%d:%d:%d:-1", item.KPID, seasonNum, epNum)})
		}
		if hasNext {
			nav = append(nav, tg.InlineKeyboardButton{Text: ">>>", CallbackData: fmt.Sprintf("epnav:%d:%d:%d:1", item.KPID, seasonNum, epNum)})
		}
		rows = append(rows, nav)
	}
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})
	kb := tg.NewInlineKeyboardMarkup(rows)

	return bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
		ChatID:      chatID,
		MessageID:   copiedID,
		ReplyMarkup: &kb,
	})
}

func buildSeasonHeader(item *storage.WatchItem, seasonNum int) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = fmt.Sprintf("kp_%d", item.KPID)
	}

	season := findSeason(item, seasonNum)
	if season == nil {
		return fmt.Sprintf("%s\nСезон %d", title, seasonNum)
	}

	baseVoice := strings.TrimSpace(item.Voice)
	baseQuality := strings.TrimSpace(item.Quality)

	if baseVoice == "" {
		baseVoice = mostCommonEpisodeValue(season, func(ep storage.Episode) string { return ep.Voice })
	}
	if baseQuality == "" {
		baseQuality = mostCommonEpisodeValue(season, func(ep storage.Episode) string { return ep.Quality })
	}

	lines := []string{
		fmt.Sprintf("%s\nСезон %d", title, seasonNum),
	}
	if baseVoice != "" {
		lines = append(lines, baseVoice)
	}
	if baseQuality != "" {
		lines = append(lines, baseQuality)
	}

	diffVoice := collectEpisodeDiffs(season, baseVoice, func(ep storage.Episode) string { return ep.Voice })
	for _, d := range diffVoice {
		lines = append(lines, d)
	}
	diffQuality := collectEpisodeDiffs(season, baseQuality, func(ep storage.Episode) string { return ep.Quality })
	for _, d := range diffQuality {
		lines = append(lines, d)
	}

	return strings.Join(lines, "\n")
}

func mostCommonEpisodeValue(season *storage.Season, pick func(storage.Episode) string) string {
	counts := map[string]int{}
	for _, ep := range season.Episodes {
		val := strings.TrimSpace(pick(ep))
		if val == "" {
			continue
		}
		counts[val]++
	}
	var best string
	bestCount := 0
	for k, v := range counts {
		if v > bestCount {
			bestCount = v
			best = k
		}
	}
	return best
}

func collectEpisodeDiffs(season *storage.Season, base string, pick func(storage.Episode) string) []string {
	base = strings.TrimSpace(base)
	if season == nil {
		return nil
	}
	byVal := map[string][]int{}
	for _, ep := range season.Episodes {
		val := strings.TrimSpace(pick(ep))
		if val == "" || val == base {
			continue
		}
		byVal[val] = append(byVal[val], ep.Number)
	}
	if len(byVal) == 0 {
		return nil
	}
	lines := []string{}
	for val, eps := range byVal {
		sort.Ints(eps)
		lines = append(lines, fmt.Sprintf("* (%s %s - %s)", formatEpisodeList(eps), seriesWord(len(eps)), val))
	}
	sort.Strings(lines)
	return lines
}

func formatEpisodeList(nums []int) string {
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

func seriesWord(count int) string {
	if count == 1 {
		return "серия"
	}
	return "серии"
}

func parseMessageIDList(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, _ := strconv.Atoi(p)
		if id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func joinMessageIDs(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func movieMessageIDs(item *storage.WatchItem) []int {
	if item == nil {
		return nil
	}
	if len(item.StorageMessageIDs) > 0 {
		ids := append([]int(nil), item.StorageMessageIDs...)
		sort.Ints(ids)
		return ids
	}
	if item.StorageMessageID > 0 {
		return []int{item.StorageMessageID}
	}
	return nil
}
