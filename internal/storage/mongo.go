package storage

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Mongo struct {
	client *mongo.Client
	col    *mongo.Collection
}

type WatchItem struct {
	ID               primitive.ObjectID `bson:"_id,omitempty"`
	KPID             int                `bson:"kp_id"`
	Type             string             `bson:"type"`
	Title            string             `bson:"title,omitempty"`
	Voice            string             `bson:"voice,omitempty"`
	Quality          string             `bson:"quality,omitempty"`
	StorageChatID    int64              `bson:"storage_chat_id,omitempty"`
	StorageMessageID int                `bson:"storage_message_id,omitempty"`
	StorageMessageIDs []int             `bson:"storage_message_ids,omitempty"`
	Seasons          []Season           `bson:"seasons,omitempty"`
	UpdatedAt        time.Time          `bson:"updated_at"`
}

type Season struct {
	Number   int       `bson:"number"`
	Episodes []Episode `bson:"episodes"`
}

type EpisodeVariant struct {
	StorageChatID    int64  `bson:"storage_chat_id"`
	StorageMessageID int    `bson:"storage_message_id"`
	Voice            string `bson:"voice,omitempty"`
	Quality          string `bson:"quality,omitempty"`
}

type Episode struct {
	Number           int              `bson:"number"`
	StorageChatID    int64            `bson:"storage_chat_id"`
	StorageMessageID int              `bson:"storage_message_id"`
	Voice            string           `bson:"voice,omitempty"`
	Quality          string           `bson:"quality,omitempty"`
	Variants         []EpisodeVariant `bson:"variants,omitempty"`
}

func NewMongo(ctx context.Context, uri string) (*Mongo, error) {
	if uri == "" {
		return nil, errors.New("MONGODB_URI is empty")
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	db := client.Database("neomovies")
	col := db.Collection("watch_items")
	_, _ = col.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{bson.E{Key: "kp_id", Value: 1}}, Options: options.Index().SetUnique(true)})
	return &Mongo{client: client, col: col}, nil
}

func (m *Mongo) GetWatchItemByKPID(ctx context.Context, kpID int) (*WatchItem, error) {
	if m == nil {
		return nil, errors.New("mongo not configured")
	}
	var item WatchItem
	err := m.col.FindOne(ctx, bson.M{"kp_id": kpID}).Decode(&item)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &item, err
}

func (m *Mongo) UpsertWatchMovie(ctx context.Context, kpID int, voice string, quality string, storageChatID int64, storageMessageIDs []int) error {
	if m == nil {
		return nil
	}
	storageMessageID := 0
	if len(storageMessageIDs) > 0 {
		storageMessageID = storageMessageIDs[0]
	}
	_, err := m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"kp_id":              kpID,
			"type":               "movie",
			"voice":              voice,
			"quality":            quality,
			"storage_chat_id":    storageChatID,
			"storage_message_id": storageMessageID,
			"storage_message_ids": storageMessageIDs,
			"updated_at":         time.Now(),
		}},
		options.Update().SetUpsert(true),
	)
	return err
}

func (m *Mongo) AppendMovieParts(ctx context.Context, kpID int, storageChatID int64, storageMessageIDs []int) error {
	if m == nil {
		return nil
	}
	if kpID <= 0 || storageChatID == 0 || len(storageMessageIDs) == 0 {
		return nil
	}
	item, err := m.GetWatchItemByKPID(ctx, kpID)
	if err != nil {
		return err
	}
	if item == nil {
		return errors.New("item not found")
	}
	if item.Type != "movie" {
		return errors.New("item is not movie")
	}
	if item.StorageChatID != 0 && item.StorageChatID != storageChatID {
		return errors.New("storage chat id mismatch")
	}
	item.StorageChatID = storageChatID
	ids := map[int]struct{}{}
	for _, id := range item.StorageMessageIDs {
		if id > 0 {
			ids[id] = struct{}{}
		}
	}
	if item.StorageMessageID > 0 {
		ids[item.StorageMessageID] = struct{}{}
	}
	for _, id := range storageMessageIDs {
		if id > 0 {
			ids[id] = struct{}{}
		}
	}
	merged := make([]int, 0, len(ids))
	for id := range ids {
		merged = append(merged, id)
	}
	sort.Ints(merged)
	item.StorageMessageIDs = merged
	if len(merged) > 0 {
		item.StorageMessageID = merged[0]
	}
	item.UpdatedAt = time.Now()
	_, err = m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"storage_chat_id":     item.StorageChatID,
			"storage_message_id":  item.StorageMessageID,
			"storage_message_ids": item.StorageMessageIDs,
			"updated_at":          item.UpdatedAt,
		}},
	)
	return err
}

func (m *Mongo) UpsertWatchSeries(ctx context.Context, kpID int, title string) error {
	if m == nil {
		return nil
	}
	_, err := m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"kp_id":      kpID,
			"type":       "series",
			"title":      title,
			"updated_at": time.Now(),
		}},
		options.Update().SetUpsert(true),
	)
	return err
}

func (m *Mongo) UpsertSeriesEpisode(ctx context.Context, kpID int, seasonNum int, episodeNum int, voice string, quality string, storageChatID int64, storageMessageID int) error {
	if m == nil {
		return nil
	}

	item, err := m.GetWatchItemByKPID(ctx, kpID)
	if err != nil {
		return err
	}
	if item == nil {
		item = &WatchItem{KPID: kpID, Type: "series"}
	}
	item.Type = "series"
	if item.Seasons == nil {
		item.Seasons = []Season{}
	}

	seasonIdx := -1
	for i := range item.Seasons {
		if item.Seasons[i].Number == seasonNum {
			seasonIdx = i
			break
		}
	}
	if seasonIdx == -1 {
		item.Seasons = append(item.Seasons, Season{Number: seasonNum, Episodes: []Episode{}})
		seasonIdx = len(item.Seasons) - 1
	}

	eps := item.Seasons[seasonIdx].Episodes
	epIdx := -1
	for i := range eps {
		if eps[i].Number == episodeNum {
			epIdx = i
			break
		}
	}
	newVar := EpisodeVariant{
		StorageChatID:    storageChatID,
		StorageMessageID: storageMessageID,
		Voice:            strings.TrimSpace(voice),
		Quality:          strings.TrimSpace(quality),
	}
	newEp := Episode{
		Number:           episodeNum,
		StorageChatID:    storageChatID,
		StorageMessageID: storageMessageID,
		Voice:            newVar.Voice,
		Quality:          newVar.Quality,
		Variants:         []EpisodeVariant{newVar},
	}
	if epIdx == -1 {
		eps = append(eps, newEp)
	} else {
		ep := eps[epIdx]
		if len(ep.Variants) == 0 {
			if ep.StorageChatID != 0 && ep.StorageMessageID != 0 {
				ep.Variants = append(ep.Variants, EpisodeVariant{
					StorageChatID:    ep.StorageChatID,
					StorageMessageID: ep.StorageMessageID,
					Voice:            strings.TrimSpace(ep.Voice),
					Quality:          strings.TrimSpace(ep.Quality),
				})
			}
		}
		dup := false
		for _, v := range ep.Variants {
			if v.StorageChatID == newVar.StorageChatID &&
				v.StorageMessageID == newVar.StorageMessageID &&
				strings.EqualFold(strings.TrimSpace(v.Voice), newVar.Voice) &&
				strings.EqualFold(strings.TrimSpace(v.Quality), newVar.Quality) {
				dup = true
				break
			}
		}
		if !dup {
			ep.Variants = append(ep.Variants, newVar)
		}
		// Keep legacy fields in sync with first variant
		if len(ep.Variants) > 0 {
			ep.StorageChatID = ep.Variants[0].StorageChatID
			ep.StorageMessageID = ep.Variants[0].StorageMessageID
			ep.Voice = ep.Variants[0].Voice
			ep.Quality = ep.Variants[0].Quality
		}
		eps[epIdx] = ep
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Number < eps[j].Number })
	item.Seasons[seasonIdx].Episodes = eps
	item.UpdatedAt = time.Now()

	sort.Slice(item.Seasons, func(i, j int) bool { return item.Seasons[i].Number < item.Seasons[j].Number })

	_, err = m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"kp_id":      item.KPID,
			"type":       item.Type,
			"title":      item.Title,
			"seasons":    item.Seasons,
			"updated_at": item.UpdatedAt,
		}},
		options.Update().SetUpsert(true),
	)
	return err
}

func (m *Mongo) DeleteSeriesEpisode(ctx context.Context, kpID int, seasonNum int, episodeNum int) error {
	if m == nil {
		return nil
	}
	item, err := m.GetWatchItemByKPID(ctx, kpID)
	if err != nil || item == nil {
		return err
	}
	updated := false
	for si := range item.Seasons {
		if item.Seasons[si].Number != seasonNum {
			continue
		}
		eps := item.Seasons[si].Episodes
		out := make([]Episode, 0, len(eps))
		for _, ep := range eps {
			if ep.Number == episodeNum {
				updated = true
				continue
			}
			out = append(out, ep)
		}
		item.Seasons[si].Episodes = out
		break
	}
	if !updated {
		return nil
	}
	item.UpdatedAt = time.Now()
	_, err = m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"seasons":    item.Seasons,
			"updated_at": item.UpdatedAt,
		}},
	)
	return err
}

func (m *Mongo) DeleteSeason(ctx context.Context, kpID int, seasonNum int) error {
	if m == nil {
		return nil
	}
	item, err := m.GetWatchItemByKPID(ctx, kpID)
	if err != nil || item == nil {
		return err
	}
	out := make([]Season, 0, len(item.Seasons))
	removed := false
	for _, s := range item.Seasons {
		if s.Number == seasonNum {
			removed = true
			continue
		}
		out = append(out, s)
	}
	if !removed {
		return nil
	}
	item.Seasons = out
	item.UpdatedAt = time.Now()
	_, err = m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"seasons":    item.Seasons,
			"updated_at": item.UpdatedAt,
		}},
	)
	return err
}

func (m *Mongo) DeleteByKPID(ctx context.Context, kpID int) error {
	if m == nil {
		return nil
	}
	_, err := m.col.DeleteOne(ctx, bson.M{"kp_id": kpID})
	return err
}

func (m *Mongo) ListRecent(ctx context.Context, limit int) ([]WatchItem, error) {
	if m == nil {
		return nil, errors.New("mongo not configured")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	opts := options.Find().SetSort(bson.D{bson.E{Key: "updated_at", Value: -1}}).SetLimit(int64(limit))
	cur, err := m.col.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	items := make([]WatchItem, 0, limit)
	for cur.Next(ctx) {
		var it WatchItem
		if err := cur.Decode(&it); err != nil {
			continue
		}
		items = append(items, it)
	}
	return items, cur.Err()
}
