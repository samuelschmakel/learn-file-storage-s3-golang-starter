package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// Get video metadata from database
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't get the video's metadata from the database", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	// Parse the uploaded video file from the form data
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", nil)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Invalid file type", nil)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy contents", nil)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't reset file pointer", nil)
		return
	}

	fmt.Printf("Here's the name of the tempFile: %s\n", tempFile.Name())
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get aspect ratio", err)
		return
	}

	fileKey := getAssetPath(mediaType)
	fmt.Printf("aspect ratio: %s\n", aspectRatio)

	if aspectRatio == "16:9" {
		fileKey = "landscape/" + fileKey
	} else if aspectRatio == "9:16" {
		fileKey = "portrait/" + fileKey
	} else {
		fileKey = "other/" + fileKey
	}

	processedFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get aspect ratio", err)
		return
	}
	defer os.Remove(processedFilePath)

	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open processed file", err)
		return
	}
	defer processedFile.Close()

	// Read first 200 bytes to check for moov
	buffer := make([]byte, 200)
	_, err = processedFile.Read(buffer)
	if err != nil {
		fmt.Println("Error!")
	}
	fmt.Printf("First 200 bytes: %x\n", buffer)

	_, err = processedFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to reset file pointer", err)
		return
	}

	// Generate 16-byte slice (32 characters in hex)
	buf := make([]byte, 200)
	_, err = processedFile.Read(buf)
	if err != nil {
		log.Printf("Error reading processed file: %v", err)
	} else {
		log.Printf("First 200 bytes of processed file before S3 upload: %x", buf)
	}
	// Don't forget to seek back to start of file
	processedFile.Seek(0, 0)

	params := &s3.PutObjectInput{
		Bucket:       aws.String(cfg.s3Bucket),
		Key:          aws.String(fileKey),
		Body:         processedFile,
		ContentType:  aws.String("video/mp4"),
		CacheControl: aws.String("public, max-age=31536000"),
	}

	_, err = cfg.s3Client.PutObject(context.TODO(), params)
	if err != nil {
		log.Printf("Failed to upload file: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to upload object to S3", nil)
		return
	}

	// Update the VideoURL of the video record in the database
	vidURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileKey)
	video.VideoURL = &vidURL

	fmt.Printf("Here's the updated URL: %s\n", vidURL)
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update database with new thumbnail url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
