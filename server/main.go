package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"

	trackpb "../proto"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TrackServiceServer struct {
}

var db *mongo.Client
var trackdb *mongo.Collection
var mongoCtx context.Context

func (s *TrackServiceServer) ReadUser(ctx context.Context, req *trackpb.ReadUserReq) (*trackpb.ReadUserRes, error) {
	// convert string id (from proto) to mongoDB ObjectId
	oid, err := primitive.ObjectIDFromHex(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Could not convert to ObjectId: %v", err))
	}
	result := trackdb.FindOne(ctx, bson.M{"_id": oid})
	// Create an empty UserItem to write our decode result to
	data := UserItem{}
	// decode and write to data
	if err := result.Decode(&data); err != nil {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("Could not find User with Object Id %s: %v", req.GetId(), err))
	}
	// Cast to ReadUserRes type
	response := &trackpb.ReadUserRes{
		User: &trackpb.User{
			Id:     oid.Hex(),
			Name:   data.Name,
			Email:  data.Email,
			Mobile: data.Mobile,
		},
	}
	return response, nil
}

func (s *TrackServiceServer) CreateUser(ctx context.Context, req *trackpb.CreateUserReq) (*trackpb.CreateUserRes, error) {
	// Get the protobuf blog type from the protobuf request type
	// Essentially doing req.User to access the struct with a nil check
	user := req.GetUser()
	// Now we have to convert this into a UserItem type to convert into BSON
	data := UserItem{
		Name:   user.GetName(),
		Email:  user.GetEmail(),
		Mobile: user.GetMobile(),
	}

	// Insert the data into the database
	// *InsertOneResult contains the oid
	result, err := trackdb.InsertOne(mongoCtx, data)
	// check error
	if err != nil {
		// return internal gRPC error to be handled later
		return nil, status.Errorf(
			codes.Internal,
			fmt.Sprintf("Internal error: %v", err),
		)
	}
	// add the id to user
	oid := result.InsertedID.(primitive.ObjectID)
	user.Id = oid.Hex()
	// return the user in a CreateUserRes type
	return &trackpb.CreateUserRes{User: user}, nil
}

func (s *TrackServiceServer) ListUsers(req *trackpb.ListUsersReq, stream trackpb.TrackService_ListUsersServer) error {
	// Initiate a UserItem type to write decoded data to
	data := &UserItem{}
	// collection.Find returns a cursor for our (empty) query
	cursor, err := trackdb.Find(context.Background(), bson.M{})
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("Unknown internal error: %v", err))
	}
	// An expression with defer will be called at the end of the function
	defer cursor.Close(context.Background())
	// cursor.Next() returns a boolean, if false there are no more items and loop will break
	for cursor.Next(context.Background()) {
		// Decode the data at the current pointer and write it to data
		err := cursor.Decode(data)
		// check error
		if err != nil {
			return status.Errorf(codes.Unavailable, fmt.Sprintf("Could not decode data: %v", err))
		}
		// If no error is found send blog over stream
		stream.Send(&trackpb.ListUsersRes{
			User: &trackpb.User{
				Id:     data.ID.Hex(),
				Name:   data.Name,
				Email:  data.Email,
				Mobile: data.Mobile,
			},
		})
	}
	// Check if the cursor has any errors
	if err := cursor.Err(); err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("Unkown cursor error: %v", err))
	}
	return nil
}

func (s *TrackServiceServer) CreateActivity(ctx context.Context, req *trackpb.CreateActivityReq) (*trackpb.CreateActivityRes, error) {
	// Get the protobuf Activity from the protobuf request type
	// Essentially doing req.Activity to access the struct with a nil check
	activity := req.GetActivity()
	// Now we have to convert this into a ActivityItem type to convert into BSON
	data := ActivityItem{
		Type:     activity.GetType(),
		Duration: activity.GetDuration(),
		Label:    activity.GetLabel(),
	}

	// Insert the data into the database
	// *InsertOneResult contains the oid
	result, err := trackdb.InsertOne(mongoCtx, data)
	// check error
	if err != nil {
		// return internal gRPC error to be handled later
		return nil, status.Errorf(
			codes.Internal,
			fmt.Sprintf("Internal error: %v", err),
		)
	}
	// add the id to Activity
	oid := result.InsertedID.(primitive.ObjectID)
	activity.Id = oid.Hex()
	// return the activity in a CreateActivityRes type
	return &trackpb.CreateActivityRes{Activity: activity}, nil
}

func (s *TrackServiceServer) UpdateActivity(ctx context.Context, req *trackpb.UpdateActivityReq) (*trackpb.UpdateActivityRes, error) {
	// Get the activity data from the request
	activity := req.GetActivity()

	// Convert the Id string to a MongoDB ObjectId
	oid, err := primitive.ObjectIDFromHex(activity.GetId())
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			fmt.Sprintf("Could not convert the supplied blog id to a MongoDB ObjectId: %v", err),
		)
	}

	// Convert the data to be updated into an unordered Bson document
	update := bson.M{
		"type":     activity.GetType(),
		"duration": activity.GetDuration(),
		"label":    activity.GetLabel(),
	}

	// Convert the oid into an unordered bson document to search by id
	filter := bson.M{"_id": oid}

	// Result is the BSON encoded result
	// To return the updated document instead of original we have to add options.
	result := trackdb.FindOneAndUpdate(ctx, filter, bson.M{"$set": update}, options.FindOneAndUpdate().SetReturnDocument(1))

	// Decode result and write it to 'decoded'
	decoded := ActivityItem{}
	err = result.Decode(&decoded)
	if err != nil {
		return nil, status.Errorf(
			codes.NotFound,
			fmt.Sprintf("Could not find blog with supplied ID: %v", err),
		)
	}
	return &trackpb.UpdateActivityRes{
		Activity: &trackpb.Activity{
			Id:       decoded.ID.Hex(),
			Type:     decoded.Type,
			Duration: decoded.Duration,
			Label:    decoded.Label,
		},
	}, nil
}

func (s *TrackServiceServer) ListActivitys(req *trackpb.ListActivitysReq, stream trackpb.TrackService_ListActivitysServer) error {
	// Initiate a ActivityItem type to write decoded data to
	data := &ActivityItem{}
	// collection.Find returns a cursor for our (empty) query
	cursor, err := trackdb.Find(context.Background(), bson.M{})
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("Unknown internal error: %v", err))
	}
	// An expression with defer will be called at the end of the function
	defer cursor.Close(context.Background())
	// cursor.Next() returns a boolean, if false there are no more items and loop will break
	for cursor.Next(context.Background()) {
		// Decode the data at the current pointer and write it to data
		err := cursor.Decode(data)
		// check error
		if err != nil {
			return status.Errorf(codes.Unavailable, fmt.Sprintf("Could not decode data: %v", err))
		}
		// If no error is found send activity over stream
		stream.Send(&trackpb.ListActivitysRes{
			Activity: &trackpb.Activity{
				Id:       data.ID.Hex(),
				Type:     data.Type,
				Duration: data.Duration,
				Label:    data.Label,
			},
		})
	}
	// Check if the cursor has any errors
	if err := cursor.Err(); err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("Unkown cursor error: %v", err))
	}
	return nil
}

func (s *TrackServiceServer) IsBool(ctx context.Context, req *trackpb.IsBoolReq) (*trackpb.IsBoolRes, error) {
	// convert string id (from proto) to mongoDB ObjectId
	oid, err := primitive.ObjectIDFromHex(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Could not convert to ObjectId: %v", err))
	}
	result := trackdb.FindOne(ctx, bson.M{"_id": oid})
	// Create an empty UserItem to write our decode result to
	data := ActivityItem{}
	// decode and write to data
	if err := result.Decode(&data); err != nil {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("Could not find User with Object Id %s: %v", req.GetId(), err))
	}

	if data.Label == "Done" {
		response := &trackpb.IsBoolRes{
			Result: true,
		}
		return response, nil
	} else {
		response := &trackpb.IsBoolRes{
			Result: false,
		}
		return response, nil
	}
}

func (s *TrackServiceServer) IsValid(ctx context.Context, req *trackpb.IsValidReq) (*trackpb.IsValidRes, error) {
	// convert string id (from proto) to mongoDB ObjectId
	oid, err := primitive.ObjectIDFromHex(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Could not convert to ObjectId: %v", err))
	}
	result := trackdb.FindOne(ctx, bson.M{"_id": oid})
	// Create an empty UserItem to write our decode result to
	data := ActivityItem{}
	// decode and write to data
	if err := result.Decode(&data); err != nil {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("Could not find User with Object Id %s: %v", req.GetId(), err))
	}

	// Play Event will be valid if Duration is greater than 3 hours and less than 14 hours
	if data.Type == "Play" {
		if data.Duration > 3 && data.Duration < 14 {
			response := &trackpb.IsValidRes{
				Valid: true,
			}
			return response, nil
		} else {
			response := &trackpb.IsValidRes{
				Valid: false,
			}
			return response, nil
		}
	}

	// Sleep Event will be valid if Duration is greater than 3 hours and less than or equal to 8 hours
	if data.Type == "Sleep" {

		if data.Duration > 3 && data.Duration <= 8 {
			response := &trackpb.IsValidRes{
				Valid: true,
			}
			return response, nil
		} else {
			response := &trackpb.IsValidRes{
				Valid: false,
			}
			return response, nil
		}
	}

	// Read Event will be valid if Duration is greater than 3 hours and less than or equal to 5 hours
	if data.Type == "Read" {
		if data.Duration > 3 && data.Duration <= 5 {
			response := &trackpb.IsValidRes{
				Valid: true,
			}
			return response, nil
		} else {
			response := &trackpb.IsValidRes{
				Valid: false,
			}
			return response, nil
		}
	}

	// Eat Event will be valid if Duration is greater than 1 hours and less than or equal to 3 hours
	if data.Type == "Eat" {
		if data.Duration > 1 && data.Duration <= 3 {
			response := &trackpb.IsValidRes{
				Valid: true,
			}
			return response, nil
		} else {
			response := &trackpb.IsValidRes{
				Valid: false,
			}
			return response, nil
		}
	}

	response := &trackpb.IsValidRes{
		Valid: false,
	}
	return response, nil
}

type UserItem struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	Name   string             `bson:"name"`
	Email  string             `bson:"email"`
	Mobile string             `bson:"mobile"`
}

type ActivityItem struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	Type     string             `bson:"type"`
	Duration int64              `bson:"duration"`
	Label    string             `bson:"label"`
}

func main() {

	// Configure 'log' package to give file name and line number on eg. log.Fatal
	// just the filename & line number:
	// log.SetFlags(log.Lshortfile)
	// Or add timestamps and pipe file name and line number to it:
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	fmt.Println("Starting server on port :50051...")

	// 50051 is the default port for gRPC
	// Ideally we'd use 0.0.0.0 instead of localhost as well
	listener, err := net.Listen("tcp", ":50051")

	if err != nil {
		log.Fatalf("Unable to listen on port :50051: %v", err)
	}

	// slice of gRPC options
	// Here we can configure things like TLS
	opts := []grpc.ServerOption{}
	// var s *grpc.Server
	s := grpc.NewServer(opts...)
	// var srv *TrackServiceServer
	srv := &TrackServiceServer{}

	trackpb.RegisterTrackServiceServer(s, srv)

	// Initialize MongoDb client
	fmt.Println("Connecting to MongoDB...")
	mongoCtx = context.Background()
	db, err = mongo.Connect(mongoCtx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping(mongoCtx, nil)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v\n", err)
	} else {
		fmt.Println("Connected to Mongodb")
	}

	trackdb = db.Database("mydb").Collection("track")

	// Start the server in a child routine
	go func() {
		if err := s.Serve(listener); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()
	fmt.Println("Server succesfully started on port :50051")

	// Create a channel to receive OS signals
	c := make(chan os.Signal)

	// Relay os.Interrupt to our channel (os.Interrupt = CTRL+C)
	// Ignore other incoming signals
	signal.Notify(c, os.Interrupt)

	// Block main routine until a signal is received
	// As long as user doesn't press CTRL+C a message is not passed
	// And our main routine keeps running
	// If the main routine were to shutdown so would the child routine that is Serving the server
	<-c

	// After receiving CTRL+C Properly stop the server
	fmt.Println("\nStopping the server...")
	s.Stop()
	listener.Close()
	fmt.Println("Closing MongoDB connection")
	db.Disconnect(mongoCtx)
	fmt.Println("Done.")
}
