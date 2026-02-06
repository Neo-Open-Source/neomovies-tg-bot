package storage

import (
	"context"
	"errors"
	"sort"
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
	StorageChatID    int64              `bson:"storage_chat_id,omitempty"`
	StorageMessageID int                `bson:"storage_message_id,omitempty"`
	Seasons          []Season           `bson:"seasons,omitempty"`
	UpdatedAt        time.Time          `bson:"updated_at"`
}

type Season struct {
	Number   int       `bson:"number"`
	Episodes []Episode `bson:"episodes"`
}

type Episode struct {
	Number           int   `bson:"number"`
	StorageChatID    int64 `bson:"storage_chat_id"`
	StorageMessageID int   `bson:"storage_message_id"`
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

func (m *Mongo) UpsertWatchMovie(ctx context.Context, kpID int, storageChatID int64, storageMessageID int) error {
	if m == nil {
		return nil
	}
	_, err := m.col.UpdateOne(ctx,
		bson.M{"kp_id": kpID},
		bson.M{"$set": bson.M{
			"kp_id":              kpID,
			"type":               "movie",
			"storage_chat_id":    storageChatID,
			"storage_message_id": storageMessageID,
			"updated_at":         time.Now(),
		}},
		options.Update().SetUpsert(true),
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

func (m *Mongo) UpsertSeriesEpisode(ctx context.Context, kpID int, seasonNum int, episodeNum int, storageChatID int64, storageMessageID int) error {
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
	newEp := Episode{Number: episodeNum, StorageChatID: storageChatID, StorageMessageID: storageMessageID}
	if epIdx == -1 {
		eps = append(eps, newEp)
	} else {
		eps[epIdx] = newEp
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
