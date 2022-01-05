package services

import (
	"encoder/application/repositories"
	"encoder/application/services/download_service"
	"encoder/application/services/upload_service"
	"encoder/application/utils"
	"encoder/domain"
	"errors"
	"os"
	"strconv"
)

type JobUseCase interface {
	Start() error
}

type JobService struct {
	Job                    *domain.Job
	Video                  *domain.Video
	JobRepository          repositories.JobRepository
	DownloadUseCase        download_service.DownloadUseCase
	FragmentUseCase        download_service.FragmentUseCase
	EncodeUseCase          download_service.EncodeUseCase
	RemoveTempFilesUseCase download_service.RemoveTempFilesUseCase
	UploadWorkersUseCase   upload_service.UploadWorkersUseCase
}

func NewJobService(
	job *domain.Job,
	jobRepository repositories.JobRepository,
	downloadUseCase download_service.DownloadUseCase,
	fragmentUseCase download_service.FragmentUseCase,
	encodeUseCase download_service.EncodeUseCase,
	removeTempFilesUseCase download_service.RemoveTempFilesUseCase,
	uploadWorkersUseCase upload_service.UploadWorkersUseCase,
) *JobService {
	return &JobService{
		Job:                    job,
		JobRepository:          jobRepository,
		DownloadUseCase:        downloadUseCase,
		FragmentUseCase:        fragmentUseCase,
		EncodeUseCase:          encodeUseCase,
		RemoveTempFilesUseCase: removeTempFilesUseCase,
		UploadWorkersUseCase:   uploadWorkersUseCase,
	}
}

func (j *JobService) Start() error {
	var err error

	err = j.changeJobStatus(domain.StatusDownloading)
	if err != nil {
		return j.failJob(err)
	}
	err = j.DownloadUseCase.Execute(os.Getenv(utils.InputBucketName))
	if err != nil {
		return j.failJob(err)
	}

	err = j.changeJobStatus(domain.StatusFragmenting)
	if err != nil {
		return j.failJob(err)
	}
	err = j.FragmentUseCase.Execute()
	if err != nil {
		return j.failJob(err)
	}

	err = j.changeJobStatus(domain.StatusEncoding)
	if err != nil {
		return j.failJob(err)
	}
	err = j.EncodeUseCase.Execute()
	if err != nil {
		return j.failJob(err)
	}

	err = j.changeJobStatus(domain.StatusUploading)
	if err != nil {
		return j.failJob(err)
	}
	err = j.performUpload()
	if err != nil {
		j.failJob(err)
	}

	err = j.changeJobStatus(domain.StatusRemovingFiles)
	if err != nil {
		return j.failJob(err)
	}
	err = j.RemoveTempFilesUseCase.Execute()
	if err != nil {
		return j.failJob(err)
	}

	err = j.changeJobStatus(domain.StatusFinished)
	if err != nil {
		return j.failJob(err)
	}

	return nil
}

func (j JobService) changeJobStatus(status string) error {
	var err error

	j.Job.Status = status
	j.Job, err = j.JobRepository.Update(j.Job)

	if err != nil {
		return j.failJob(err)
	}

	return nil
}

func (j *JobService) failJob(error error) error {
	j.Job.Status = domain.StatusFailed
	j.Job.Error = error.Error()

	_, err := j.JobRepository.Update(j.Job)
	if err != nil {
		return err
	}

	return nil
}

func (j *JobService) performUpload() error {

	err := j.changeJobStatus(domain.StatusUploading)
	if err != nil {
		return j.failJob(err)
	}

	videoPath := os.Getenv(utils.LocalStoragePath) + "/" + j.Video.ID
	concurrency, _ := strconv.Atoi(os.Getenv(utils.ConcurrencyUpload))
	doneUpload := make(chan string)

	uploadService := upload_service.NewUploadService(utils.OutputBucketName)
	uploadWorkers := upload_service.NewUploadWorkersService(uploadService, videoPath)

	go uploadWorkers.Execute(concurrency, doneUpload)

	var uploadResult string
	uploadResult = <-doneUpload

	if uploadResult != utils.UploadCompleted {
		return j.failJob(errors.New(uploadResult))
	}

	return err
}
