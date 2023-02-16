package speedtest

import (
	"context"
	"github.com/LyricTian/queue"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type (
	downloadWarmUpFunc func(context.Context, *http.Client, string) error
	downloadFunc       func(context.Context, *http.Client, string, int) error
	uploadWarmUpFunc   func(context.Context, *http.Client, string) error
	uploadFunc         func(context.Context, *http.Client, string, int) error
)

var (
	dlSizes = [...]int{350, 500, 750, 1000, 1500, 2000, 2500, 3000, 3500, 4000}
	ulSizes = [...]int{100, 300, 500, 800, 1000, 1500, 2500, 3000, 3500, 4000} // kB
)

const testTime = time.Second * 10

// DownloadTest executes the test to measure download speed
func (s *Server) DownloadTest(savingMode bool) error {
	return s.downloadTestContext(context.Background(), savingMode, dlWarmUp, downloadRequest)
}

// DownloadTestContext executes the test to measure download speed, observing the given context.
func (s *Server) DownloadTestContext(ctx context.Context, savingMode bool) error {
	return s.downloadTestContext(ctx, savingMode, dlWarmUp, downloadRequest)
}

func testHandler(captureFunc func() *time.Ticker, job queue.Jober) {
	// When the number of processor cores is equivalent to the processing program,
	// the processing efficiency reaches the highest level (VT is not considered).
	q := queue.NewQueue(10, runtime.NumCPU())
	q.Run()

	ticker := captureFunc()
	time.AfterFunc(testTime, func() {
		ticker.Stop()
		q.Terminate()
	})

	for i := 0; i < 1000; i++ {
		q.Push(job)
	}
}

func (s *Server) downloadTestContext(
	ctx context.Context,
	savingMode bool,
	dlWarmUp downloadWarmUpFunc,
	downloadRequest downloadFunc,
) error {
	dlURL := strings.Split(s.URL, "/upload.php")[0]
	testHandler(GlobalDataManager.DownloadRateCapture, queue.NewJob("downLink", func(v interface{}) {
		_ = downloadRequest(ctx, s.doer, dlURL, 5)
	}))
	return nil
}

// UploadTest executes the test to measure upload speed
func (s *Server) UploadTest(savingMode bool) error {
	return s.uploadTestContext(context.Background(), savingMode, ulWarmUp, uploadRequest)
}

// UploadTestContext executes the test to measure upload speed, observing the given context.
func (s *Server) UploadTestContext(ctx context.Context, savingMode bool) error {
	return s.uploadTestContext(ctx, savingMode, ulWarmUp, uploadRequest)
}

func (s *Server) uploadTestContext(
	ctx context.Context,
	savingMode bool,
	ulWarmUp uploadWarmUpFunc,
	uploadRequest uploadFunc,
) error {
	testHandler(GlobalDataManager.UploadRateCapture, queue.NewJob("upLink", func(v interface{}) {
		_ = uploadRequest(ctx, s.doer, s.URL, 5)
	}))
	return nil
}

func dlWarmUp(ctx context.Context, doer *http.Client, dlURL string) error {
	return downloadRequest(ctx, doer, dlURL, 2)
}

func ulWarmUp(ctx context.Context, doer *http.Client, ulURL string) error {
	return uploadRequest(ctx, doer, ulURL, 4)
}

func downloadRequest(ctx context.Context, doer *http.Client, dlURL string, w int) error {
	size := dlSizes[w]
	xdlURL := dlURL + "/random" + strconv.Itoa(size) + "x" + strconv.Itoa(size) + ".jpg"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xdlURL, nil)
	if err != nil {
		return err
	}

	resp, err := doer.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return GlobalDataManager.NewDataChunk().DownloadSnapshotHandler(resp.Body)
}

func uploadRequest(ctx context.Context, doer *http.Client, ulURL string, w int) error {
	size := ulSizes[w]

	dc := GlobalDataManager.NewDataChunk().UploadSnapshotHandler((size*100 - 51) * 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ulURL, dc)
	req.ContentLength = dc.ContentLength
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := doer.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)

	return err
}

// PingTest executes test to measure latency
func (s *Server) PingTest() error {
	return s.PingTestContext(context.Background())
}

// PingTestContext executes test to measure latency, observing the given context.
func (s *Server) PingTestContext(ctx context.Context) error {
	pingURL := strings.Split(s.URL, "/upload.php")[0] + "/latency.txt"

	l := time.Second * 10
	for i := 0; i < 3; i++ {
		sTime := time.Now()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
		if err != nil {
			return err
		}

		resp, err := s.doer.Do(req)
		if err != nil {
			return err
		}

		fTime := time.Now()
		if fTime.Sub(sTime) < l {
			l = fTime.Sub(sTime)
		}

		resp.Body.Close()
	}

	s.Latency = time.Duration(int64(l.Nanoseconds() / 2))

	return nil
}
