package main

import (
	"context"
	"errors"
	"log"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var (
	// client   *mongo.Client
	dataBase *mongo.Database

	usersCollection        *mongo.Collection
	banLogs                *mongo.Collection
	chatMessages           *mongo.Collection
	chatSettingsCollection *mongo.Collection

	upserOptions *options.UpdateOptions
)

// DB schema
// uid - index int64
// counter - inc counter
// voteCounter - inc counter

const (
	VOTE_RATING_MULTIPLY = 10
)

type UserRecord struct {
	ID          primitive.ObjectID `bson:"_id"`
	Uid         int64
	Counter     uint32
	VoteCounter uint32
	Username    string
	AltUsername string
}

type ChatMessage struct {
	MessageID int64
	ChatID    int64
	UserID    int64
	UserName  string
	Text      string
	Date      uint64
}

type ScoreResult struct {
	Rating int   `bson:"rating"`
	Userid int64 `bson:"userid"`
}

type DyncmicSetting struct {
	ChatID        int64
	Pause         bool
	LogRecipients []int64
}

func initDb(ctx context.Context, connectionLine string, dbName string) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionLine))
	if err != nil {
		panic(err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		panic(err)
	}
	log.Printf("MongoDB is connected")
	upserOptions = &options.UpdateOptions{}
	upserOptions.SetUpsert(true)
	dataBase = client.Database(dbName)
	usersCollection = dataBase.Collection("users")
	banLogs = dataBase.Collection("ban_log")
	chatMessages = dataBase.Collection("messages")
	chatSettingsCollection = dataBase.Collection("settings")
}

func userPlusOneMessage(ctx context.Context, uID int64, username string, altname string) {
	filter := bson.D{
		{Key: "uid", Value: uID},
	}
	update := bson.D{
		{Key: "$inc", Value: bson.D{
			{Key: "counter", Value: 1},
		}},
	}
	if len(username) != 0 {
		update = append(
			update,
			bson.E{
				Key:   "$set",
				Value: bson.D{{Key: "username", Value: strings.ToLower(username)}},
			},
		)
	}
	update = append(
		update,
		bson.E{
			Key:   "$set",
			Value: bson.D{{Key: "altUsername", Value: altname}},
		},
	)
	_, err := usersCollection.UpdateOne(ctx, filter, update, upserOptions)
	if err != nil {
		log.Printf("Upsert of user counter went wrong %v", err)
	}
}

func userMakeVote(ctx context.Context, uID int64, amount int) {
	filter := bson.D{
		{Key: "uid", Value: uID},
	}
	update := bson.D{
		{Key: "$inc", Value: bson.D{
			{Key: "voteCounter", Value: amount},
		}},
	}
	_, err := usersCollection.UpdateOne(ctx, filter, update, upserOptions)
	if err != nil {
		log.Printf("Upsert of user vote maker went wrong %v", err)
	}
}

func getRatingFromUserID(ctx context.Context, uID int64) (score *ScoreResult, err error) {
	filter := bson.D{
		{Key: "uid", Value: uID},
	}
	result := usersCollection.FindOne(ctx, filter)
	var user UserRecord
	err = result.Decode(&user)
	if err != nil {
		log.Printf("Can't get user score %v", err)
		return nil, err
	}

	score = &ScoreResult{
		Rating: int(user.Counter + user.VoteCounter*VOTE_RATING_MULTIPLY),
		Userid: user.Uid,
	}
	return score, nil
}

func getRatingFromUsername(ctx context.Context, username string) (score *ScoreResult, err error) {
	filter := bson.D{
		{Key: "username", Value: strings.ToLower(username)},
	}
	result := usersCollection.FindOne(ctx, filter)
	var user UserRecord
	err = result.Decode(&user)
	if err != nil {
		log.Printf("Can't get user score %v", err)
		return nil, err
	}

	score = &ScoreResult{
		Rating: int(user.Counter + user.VoteCounter*VOTE_RATING_MULTIPLY),
		Userid: user.Uid,
	}
	return score, nil
}

func getUser(ctx context.Context, uID int64) (userRecord *UserRecord, err error) {
	filter := bson.D{
		{Key: "uid", Value: uID},
	}
	result := usersCollection.FindOne(ctx, filter)
	var user UserRecord
	err = result.Decode(&user)
	if err != nil {
		log.Printf("Can't get user score %v", err)
		return nil, err
	}
	return &user, nil
}

func pushBanLog(ctx context.Context, uID int64, userInfo string, from int64) {

}

func saveMessage(ctx context.Context, message *ChatMessage) {
	_, err := chatMessages.InsertOne(ctx, message)
	if err != nil {
		log.Printf("Can't insert message %v", err)
		return
	}
}

func getMessageInfo(ctx context.Context, chatID int64, messageID int64) (chatMessage *ChatMessage, err error) {
	filter := bson.D{
		{Key: "chatid", Value: chatID},
		{Key: "messageid", Value: messageID},
	}
	result := chatMessages.FindOne(ctx, filter)
	var message ChatMessage
	err = result.Decode(&message)
	if err != nil {
		log.Printf("Cant't get message info: %v", err)
		return nil, err
	}
	return &message, nil
}

func getRatingFromMessage(ctx context.Context, chatID int64, messageID int64) (score *ScoreResult, err error) {
	getMessage := bson.D{
		{Key: "$match",
			Value: bson.D{
				{Key: "chatid", Value: chatID},
				{Key: "messageid", Value: messageID},
			},
		},
	}
	lookupUser := bson.D{
		{Key: "$lookup",
			Value: bson.D{
				{Key: "from", Value: "users"},
				{Key: "localField", Value: "userid"},
				{Key: "foreignField", Value: "uid"},
				{Key: "as", Value: "result"},
			},
		},
	}
	unwindUser := bson.D{{Key: "$unwind", Value: bson.D{{Key: "path", Value: "$result"}}}}
	calculateRating := bson.D{
		{Key: "$set",
			Value: bson.D{
				{Key: "rating",
					Value: bson.D{
						{Key: "$add",
							Value: bson.A{
								bson.D{
									{Key: "$ifNull",
										Value: bson.A{
											"$result.counter",
											0,
										},
									},
								},
								bson.D{
									{Key: "$multiply",
										Value: bson.A{
											bson.D{
												{Key: "$ifNull",
													Value: bson.A{
														"$result.voteCounter",
														0,
													},
												},
											},
											VOTE_RATING_MULTIPLY,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	clearOutput := bson.D{
		{Key: "$project",
			Value: bson.D{
				{Key: "rating", Value: 1},
				{Key: "userid", Value: 1},
			},
		},
	}
	cursor, err := chatMessages.Aggregate(ctx, mongo.Pipeline{getMessage, lookupUser, unwindUser, calculateRating, clearOutput})
	if err != nil {
		log.Printf("Can't get proper rating for message %v", err)
		return nil, err
	}

	var results []ScoreResult
	if err = cursor.All(ctx, &results); err != nil {
		log.Printf("Problem with parsing cursor")
		return nil, errors.New("mongo: can't find message")
	}
	if len(results) == 0 {
		log.Printf("Message not found")
		return nil, errors.New("mongo: can't find message")
	}
	log.Printf("Get rating %d for user %d", results[0].Rating, results[0].Userid)
	return &results[0], nil
}

func readChatsSettings(ctx context.Context) (ret map[int64]*DyncmicSetting) {
	filter := bson.D{}
	cursor, err := chatSettingsCollection.Find(ctx, filter)
	if err != nil {
		log.Panic("Can't read settings")
	}
	ret = make(map[int64]*DyncmicSetting)
	for cursor.Next(ctx) {
		var result DyncmicSetting
		err := cursor.Decode(&result)
		if err != nil {
			log.Println(err)
			continue
		}
		ret[result.ChatID] = &result
	}
	return ret

}

func writeChatSettings(ctx context.Context, chatID int64, settings *DyncmicSetting) {
	filter := bson.D{
		{Key: "chatid", Value: chatID},
	}
	update := bson.D{
		{Key: "$set", Value: settings},
	}

	_, err := chatSettingsCollection.UpdateOne(ctx, filter, update, upserOptions)
	if err != nil {
		log.Printf("Upsert of the chat settings went wrong %v", err)
	}

}

func getUserLastNthMessages(ctx context.Context, userID int64, chatID int64, amaount uint16) (ret []ChatMessage, err error) {
	filter := bson.D{
		{Key: "chatid", Value: chatID},
		{Key: "userid", Value: userID},
	}
	options := options.Find().SetSort(bson.D{{Key: "$natural", Value: -1}}).SetLimit(int64(amaount))
	cursor, err := chatMessages.Find(ctx, filter, options)
	if err != nil {
		log.Printf("Cant't get last %dth elemets: %v", amaount, err)
		return nil, err
	}
	err = cursor.All(ctx, &ret)
	if err != nil {
		log.Printf("Cant't parse last %dth elemets: %v", amaount, err)
		return nil, err
	}
	return ret, nil
}
