package face

import (
	"context"
	"fmt"
	"sync"

	"github.com/photoview/photoview/api/database"
	"github.com/photoview/photoview/api/globalinit"
	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/scanner/tools/exif"
	"github.com/photoview/photoview/api/utils"
	"gorm.io/gorm"
)

var (
	detectorsMu sync.RWMutex
	detectors   map[int]*FaceDetector
)

func init() {
	globalinit.Register("facedetector", Initialize, Terimate)
}

func Initialize(ctx context.Context) error {
	detectorsMu.Lock()
	detectors = make(map[int]*FaceDetector)
	detectorsMu.Unlock()

	if utils.EnvDisableFaceRecognition.GetBool() {
		log.Info(ctx, "Face detection disabled", utils.EnvDisableFaceRecognition.GetName(), utils.EnvDisableFaceRecognition.GetBool())
		return nil
	}

	log.Info(ctx, "Initializing face detector")

	db := database.DB(ctx)
	if db == nil {
		return fmt.Errorf("can't load db from contenxt")
	}

	return LoadAllFaces(ctx, db)
}

func Terimate(ctx context.Context) {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	cleanDetectors()
}

func cleanDetectors() {
	for id, detector := range detectors {
		detector.Close()
		delete(detectors, id)
	}
}

func LoadAllFaces(ctx context.Context, db *gorm.DB) error {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	cleanDetectors()

	db = db.WithContext(ctx)

	var users []*models.User
	if err := db.Find(&users).Error; err != nil {
		return fmt.Errorf("can't load users from db: %w", err)
	}

	for _, user := range users {
		detector, err := NewFaceDetector()
		if err != nil {
			return fmt.Errorf("initialize detector error: %w", err)
		}

		var faces []*models.ImageFace
		if err := db.Raw("SELECT if.* FROM image_faces AS if JOIN media AS media ON if.media_id = media.id JOIN user_albums AS ua ON media.album_id = ua.album_id WHERE ua.user_id = ?", user.ID).Find(ctx, &faces).Error; err != nil {
			return fmt.Errorf("query image faces of user(%d) error: %w", user.ID, err)
		}

		detector.LoadFaces(faces)
		detectors[user.ID] = detector
	}

	return nil
}

func getDetector(userID int) (*FaceDetector, error) {
	detector, ok := detectors[userID]
	if !ok {
		var err error
		detector, err = NewFaceDetector()
		if err != nil {
			return nil, fmt.Errorf("create detector for user(%d) error: %w", err)
		}
		detectors[userID] = detector
	}

	return detector, nil
}

func DetectFaces(userID int, path string, dimension exif.Dimension) ([]*models.ImageFace, error) {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	detector, err := getDetector(userID)
	if err != nil {
		return nil, err
	}

	return detector.DetectFaces(path, dimension)
}

func UpdateFaces(userID int, faces []*models.ImageFace) error {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	detector, err := getDetector(userID)
	if err != nil {
		return err
	}

	detector.UpdateFaces(faces)
	return nil
}

func MergeFaceGroup(userID int, sourceID, dstID int) error {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	detector, err := getDetector(userID)
	if err != nil {
		return err
	}

	detector.MergeFaceGroup(sourceID, dstID)
	return nil
}

func RecognizeFaces(userID int, faceGroupsIDs []int) ([]FromToPair, error) {
	detectorsMu.Lock()
	defer detectorsMu.Unlock()

	detector, err := getDetector(userID)
	if err != nil {
		return nil, err
	}

	return detector.RecognizeFaces(faceGroupsIDs), nil
}
