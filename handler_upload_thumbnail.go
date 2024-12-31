package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't parse request body as multipart form", err)
		return
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	/*
		data, err := io.ReadAll(file)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "couldn't read the image data", err)
			return
		}
	*/

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't get the video's metadata from the database", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	fmt.Printf("cfg.assetsRoot: %s\n", cfg.assetsRoot)
	fmt.Printf("mediaType: %s", mediaType)
	filePath := filepath.Join(cfg.assetsRoot, videoID.String())

	ext := ""
	if mediaType == "image/png" {
		ext = "png"
	}
	filePath = fmt.Sprintf("%s.%s", filePath, ext)
	fmt.Printf("Here's the filepath: %s\n", filePath)
	destFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create file", nil)
		return
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy contents", nil)
		return
	}

	thumbnailURLpath := fmt.Sprintf("http://localhost:%s/%s", cfg.port, filePath)
	video.ThumbnailURL = &thumbnailURLpath

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update database with new thumbnail url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
