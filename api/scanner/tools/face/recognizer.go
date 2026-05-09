package face

import (
	"fmt"
	"slices"
	"sync"

	"github.com/Kagami/go-face"

	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/scanner/tools/exif"
	"github.com/photoview/photoview/api/utils"
)

type FaceDetector struct {
	rec *face.Recognizer

	mutex        sync.RWMutex
	descriptors  []face.Descriptor
	faceGroupIDs []int
}

func NewFaceDetector() (*FaceDetector, error) {
	rec, err := face.NewRecognizer(utils.FaceRecognitionModelsPath())
	if err != nil {
		return nil, fmt.Errorf("create recognizer error: %w", err)
	}

	return &FaceDetector{
		rec: rec,
	}, nil
}

func (fd *FaceDetector) Close() {
	fd.rec.Close()
}

// LoadFaces updates the in-memory face descriptors with the ones in the database
func (fd *FaceDetector) LoadFaces(imageFaces []*models.ImageFace) {
	fd.descriptors = make([]face.Descriptor, len(imageFaces))
	fd.faceGroupIDs = make([]int, len(imageFaces))

	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	for i, imgFace := range imageFaces {
		fd.descriptors[i] = face.Descriptor(imgFace.Descriptor)
		fd.faceGroupIDs[i] = imgFace.FaceGroupID
	}
}

// DetectFaces finds the faces in the given image and saves them to the database
func (fd *FaceDetector) DetectFaces(path string, dimension exif.Dimension) ([]*models.ImageFace, error) {
	faces, err := fd.rec.RecognizeFile(path)
	if err != nil {
		return nil, fmt.Errorf("read faces from %q error: %w", path, err)
	}

	if len(faces) == 0 {
		return nil, nil
	}

	var ret []*models.ImageFace
	for _, face := range faces {
		imageFace := models.ImageFace{
			Descriptor: models.FaceDescriptor(face.Descriptor),
			Rectangle: models.FaceRectangle{
				// Converts a pixel absolute rectangle to a relative FaceRectangle.
				MinX: float64(face.Rectangle.Min.X) / float64(dimension.Width()),
				MaxX: float64(face.Rectangle.Max.X) / float64(dimension.Width()),
				MinY: float64(face.Rectangle.Min.Y) / float64(dimension.Height()),
				MaxY: float64(face.Rectangle.Max.Y) / float64(dimension.Height()),
			},
		}

		matchedIndex := fd.classifyDescriptor(face.Descriptor)
		if matchedIndex >= 0 {
			imageFace.FaceGroupID = fd.faceGroupIDs[matchedIndex]
		}
	}

	return ret, nil
}

func (fd *FaceDetector) UpdateFaces(faces []*models.ImageFace) {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	fd.descriptors = slices.Grow(fd.descriptors, len(faces))
	fd.faceGroupIDs = slices.Grow(fd.faceGroupIDs, len(faces))

	for _, imgFace := range faces {
		fd.descriptors = append(fd.descriptors, face.Descriptor(imgFace.Descriptor))
		fd.faceGroupIDs = append(fd.faceGroupIDs, imgFace.FaceGroupID)
	}

	indexes := make([]int32, len(fd.descriptors))
	for i := range fd.descriptors {
		indexes[i] = int32(i)
	}

	fd.rec.SetSamples(fd.descriptors, indexes)

	return
}

func (fd *FaceDetector) MergeFaceGroup(sourceID int, destID int) {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	for i := range fd.faceGroupIDs {
		if fd.faceGroupIDs[i] != sourceID {
			continue
		}

		fd.faceGroupIDs[i] = destID
	}
}

type FromToPair struct {
	From int
	To   int
}

func (fd *FaceDetector) RecognizeFaces(faceGroupIDs []int) (updatedFaceGroupIDs []FromToPair) {
	unrecognizedFaceGroupMap := make(map[int]bool, len(faceGroupIDs))
	for _, id := range faceGroupIDs {
		unrecognizedFaceGroupMap[id] = true
	}

	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	var unrecognizedDescriptors, newDescriptors []face.Descriptor
	var unrecognizedFaceGroupIDs, newFaceGroupIDs []int

	for i := range fd.descriptors {
		if unrecognizedFaceGroupMap[fd.faceGroupIDs[i]] {
			unrecognizedDescriptors = append(unrecognizedDescriptors, fd.descriptors[i])
			unrecognizedFaceGroupIDs = append(unrecognizedFaceGroupIDs, fd.faceGroupIDs[i])
		} else {
			newDescriptors = append(newDescriptors, fd.descriptors[i])
			newFaceGroupIDs = append(newFaceGroupIDs, fd.faceGroupIDs[i])
		}
	}

	fd.descriptors = newDescriptors
	fd.faceGroupIDs = newFaceGroupIDs

	for i := range unrecognizedDescriptors {
		matchedIndex := fd.classifyDescriptor(unrecognizedDescriptors[i])

		if matchedIndex < 0 {
			// still no match, readd it to the list
			fd.descriptors = append(fd.descriptors, unrecognizedDescriptors[i])
			fd.faceGroupIDs = append(fd.faceGroupIDs, unrecognizedFaceGroupIDs[i])
			continue
		}

		fd.descriptors = append(fd.descriptors, unrecognizedDescriptors[i])
		fd.faceGroupIDs = append(fd.faceGroupIDs, unrecognizedFaceGroupIDs[i])
		updatedFaceGroupIDs = append(updatedFaceGroupIDs, FromToPair{
			From: unrecognizedFaceGroupIDs[i],
			To:   fd.faceGroupIDs[i],
		})
	}

	return
}

func (fd *FaceDetector) classifyDescriptor(descriptor face.Descriptor) int {
	return fd.rec.ClassifyThreshold(descriptor, 0.2)
}
