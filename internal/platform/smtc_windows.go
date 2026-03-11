//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"ben/desktop/internal/playback"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-win32api/v2/win32"
	"github.com/zzl/go-winrtapi/winrt"
)

const (
	smtcClassName                  = "Windows.Media.SystemMediaTransportControls"
	timelineClassName              = "Windows.Media.SystemMediaTransportControlsTimelineProperties"
	streamReferenceClassName       = "Windows.Storage.Streams.RandomAccessStreamReference"
	storageFileClassName           = "Windows.Storage.StorageFile"
	appMediaID                     = "Ben"
	timespanTickPerMS        int64 = 10000
	artworkVariant96               = "96_jpeg"
	storageResolveTimeout          = 2 * time.Second
)

type smtcService struct {
	mu      sync.Mutex
	session *playback.Session
	bridge  playback.CorePlaybackBridge
	updates chan playback.SessionSnapshot
	stop    chan struct{}
	done    chan struct{}
	running bool
}

type smtcRuntimeState struct {
	session *playback.Session
	bridge  playback.CorePlaybackBridge

	controls     *winrt.ISystemMediaTransportControls
	controls2    *winrt.ISystemMediaTransportControls2
	updater      *winrt.ISystemMediaTransportControlsDisplayUpdater
	musicProps   *winrt.IMusicDisplayProperties
	musicProps2  *winrt.IMusicDisplayProperties2
	timeline     *winrt.ISystemMediaTransportControlsTimelineProperties
	streamRefs   *winrt.IRandomAccessStreamReferenceStatics
	storageFiles *winrt.IStorageFileStatics
	buttonToken  winrt.EventRegistrationToken
	hookAttached bool

	lastRecordingID string
	lastSubtitle    string
	lastTitle       string
	lastArtworkPath string
	hasTrack        bool
}

func newSMTCService(session *playback.Session, bridge playback.CorePlaybackBridge) *smtcService {
	return &smtcService{session: session, bridge: bridge}
}

func (s *smtcService) Start(hwnd win32.HWND) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}

	updates := make(chan playback.SessionSnapshot, 1)
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	readyCh := make(chan error, 1)

	s.updates = updates
	s.stop = stopCh
	s.done = doneCh
	s.running = true
	s.mu.Unlock()

	go s.run(hwnd, updates, stopCh, doneCh, readyCh)

	if err := <-readyCh; err != nil {
		s.mu.Lock()
		s.running = false
		s.updates = nil
		s.stop = nil
		s.done = nil
		s.mu.Unlock()
		<-doneCh
		return err
	}
	return nil
}

func (s *smtcService) UpdatePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	s.mu.Lock()
	running := s.running
	updates := s.updates
	s.mu.Unlock()
	if !running || updates == nil {
		return
	}

	select {
	case updates <- snapshot:
	default:
		select {
		case <-updates:
		default:
		}
		select {
		case updates <- snapshot:
		default:
		}
	}
}

func (s *smtcService) Close() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	stopCh := s.stop
	doneCh := s.done
	s.running = false
	s.updates = nil
	s.stop = nil
	s.done = nil
	s.mu.Unlock()

	close(stopCh)
	<-doneCh
	return nil
}

func (s *smtcService) run(
	hwnd win32.HWND,
	updates <-chan playback.SessionSnapshot,
	stopCh <-chan struct{},
	doneCh chan<- struct{},
	readyCh chan<- error,
) {
	defer close(doneCh)

	init := winrt.InitializeMt()
	defer init.Uninitialize()

	runtimeState, err := newSMTCRuntimeState(s.session, s.bridge, hwnd)
	if err != nil {
		readyCh <- err
		return
	}
	defer runtimeState.shutdown()

	readyCh <- nil

	for {
		select {
		case <-stopCh:
			return
		case snapshot := <-updates:
			runtimeState.apply(snapshot)
		}
	}
}

func newSMTCRuntimeState(session *playback.Session, bridge playback.CorePlaybackBridge, hwnd win32.HWND) (*smtcRuntimeState, error) {
	if hwnd == 0 {
		return nil, errors.New("smtc requires a valid window handle")
	}

	hs := winrt.NewHStr(smtcClassName)
	defer hs.Dispose()

	var interop *win32.ISystemMediaTransportControlsInterop
	hr := win32.RoGetActivationFactory(hs.Ptr, &win32.IID_ISystemMediaTransportControlsInterop, unsafe.Pointer(&interop))
	if win32.FAILED(hr) {
		return nil, fmt.Errorf("smtc interop activation factory: %s", win32.HRESULT_ToString(hr))
	}
	if interop == nil {
		return nil, errors.New("smtc interop activation factory returned nil")
	}
	com.AddToScope(interop)

	var controls *winrt.ISystemMediaTransportControls
	controlsHR := interop.GetForWindow(hwnd, &winrt.IID_ISystemMediaTransportControls, unsafe.Pointer(&controls))
	if win32.FAILED(controlsHR) {
		return nil, fmt.Errorf("smtc GetForWindow: %s", win32.HRESULT_ToString(controlsHR))
	}
	if controls == nil {
		return nil, errors.New("smtc unavailable for current window")
	}
	com.AddToScope(controls)

	state := &smtcRuntimeState{
		session:  session,
		bridge:   bridge,
		controls: controls,
	}

	state.controls.Put_IsEnabled(true)
	state.controls.Put_IsPlayEnabled(true)
	state.controls.Put_IsPauseEnabled(true)
	state.controls.Put_IsStopEnabled(false)
	state.controls.Put_IsNextEnabled(true)
	state.controls.Put_IsPreviousEnabled(true)

	state.updater = state.controls.Get_DisplayUpdater()
	if state.updater != nil {
		state.updater.Put_Type(winrt.MediaPlaybackType_Music)
		state.updater.Put_AppMediaId(appMediaID)
		state.musicProps = state.updater.Get_MusicProperties()
		if state.musicProps != nil {
			var musicProps2 *winrt.IMusicDisplayProperties2
			queryHR := state.musicProps.QueryInterface(&winrt.IID_IMusicDisplayProperties2, unsafe.Pointer(&musicProps2))
			if !win32.FAILED(queryHR) && musicProps2 != nil {
				com.AddToScope(musicProps2)
				state.musicProps2 = musicProps2
			}
		}
		state.updater.Update()
	}

	var controls2 *winrt.ISystemMediaTransportControls2
	queryHR := state.controls.QueryInterface(&winrt.IID_ISystemMediaTransportControls2, unsafe.Pointer(&controls2))
	if !win32.FAILED(queryHR) && controls2 != nil {
		com.AddToScope(controls2)
		state.controls2 = controls2
		state.timeline = newTimelineProperties()
	}

	state.streamRefs = newRandomAccessStreamReferenceStatics()
	state.storageFiles = newStorageFileStatics()
	state.buttonToken = state.controls.Add_ButtonPressed(state.onButtonPressed)
	state.hookAttached = true

	return state, nil
}

func (s *smtcRuntimeState) shutdown() {
	if s.controls == nil {
		return
	}
	if s.hookAttached {
		s.controls.Remove_ButtonPressed(s.buttonToken)
	}
	s.controls.Put_IsEnabled(false)
}

func (s *smtcRuntimeState) apply(snapshot playback.SessionSnapshot) {
	if s.controls == nil {
		return
	}

	s.controls.Put_PlaybackStatus(mapPlaybackStatus(snapshot.Status))

	hasQueue := snapshot.QueueLength > 0
	hasTrack := snapshot.CurrentItem != nil
	s.controls.Put_IsPlayEnabled(hasQueue)
	s.controls.Put_IsPauseEnabled(hasTrack)
	s.controls.Put_IsStopEnabled(false)
	s.controls.Put_IsNextEnabled(hasQueue)
	s.controls.Put_IsPreviousEnabled(hasTrack)

	if !hasTrack {
		s.applyEmptyTrack()
		s.applyTimeline(0, 0)
		return
	}

	item := snapshot.CurrentItem
	artworkPath := s.resolveArtworkPath(item.RecordingID)
	if s.metadataChanged(item, artworkPath) {
		s.applyMetadata(item, artworkPath)
		s.hasTrack = true
		s.lastRecordingID = strings.TrimSpace(item.RecordingID)
		s.lastTitle = strings.TrimSpace(item.Title)
		s.lastSubtitle = strings.TrimSpace(item.Subtitle)
		s.lastArtworkPath = artworkPath
	}

	durationMS := optionalInt64Value(snapshot.DurationMS)
	positionMS := clampInt64(snapshot.PositionMS, 0, durationMS)
	s.applyTimeline(positionMS, durationMS)
}

func (s *smtcRuntimeState) applyEmptyTrack() {
	if !s.hasTrack {
		return
	}
	s.hasTrack = false
	s.lastRecordingID = ""
	s.lastTitle = ""
	s.lastSubtitle = ""
	s.lastArtworkPath = ""

	if s.updater == nil {
		return
	}
	s.updater.ClearAll()
	s.updater.Put_Type(winrt.MediaPlaybackType_Music)
	s.updater.Put_AppMediaId(appMediaID)
	s.updater.Update()
}

func (s *smtcRuntimeState) applyMetadata(item *playback.SessionItem, artworkPath string) {
	if item == nil || s.updater == nil {
		return
	}

	s.updater.Put_Type(winrt.MediaPlaybackType_Music)
	s.updater.Put_AppMediaId(appMediaID)

	if s.musicProps != nil {
		title := normalizeLabel(item.Title, "Unknown Title")
		subtitle := normalizeLabel(item.Subtitle, "Unknown Artist")
		s.musicProps.Put_Title(title)
		s.musicProps.Put_Artist(subtitle)
		s.musicProps.Put_AlbumArtist(subtitle)
	}

	if s.musicProps2 != nil {
		s.musicProps2.Put_AlbumTitle("")
		s.musicProps2.Put_TrackNumber(0)
	}

	thumbnail, err := s.resolveTrackThumbnail(artworkPath)
	if err != nil {
		log.Printf("smtc artwork resolution failed: %v", err)
	}
	s.updater.Put_Thumbnail(thumbnail)
	s.updater.Update()
}

func (s *smtcRuntimeState) resolveArtworkPath(recordingID string) string {
	if s.bridge == nil {
		return ""
	}
	result, err := s.bridge.ResolveRecordingArtwork(context.Background(), recordingID, artworkVariant96)
	if err != nil || !result.Available {
		return ""
	}
	return strings.TrimSpace(result.LocalPath)
}

func (s *smtcRuntimeState) resolveTrackThumbnail(path string) (*winrt.IRandomAccessStreamReference, error) {
	if s.streamRefs == nil {
		return nil, errors.New("random access stream reference factory unavailable")
	}
	if s.storageFiles == nil {
		return nil, errors.New("storage file factory unavailable")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat artwork path %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("artwork path is a directory: %q", path)
	}

	storageFile, err := s.resolveStorageFile(path)
	if err != nil {
		return nil, err
	}
	thumbnail := s.streamRefs.CreateFromFile(storageFile)
	if thumbnail == nil {
		return nil, fmt.Errorf("CreateFromFile returned nil for %q", path)
	}
	return thumbnail, nil
}

func (s *smtcRuntimeState) resolveStorageFile(path string) (*winrt.IStorageFile, error) {
	if s.storageFiles == nil {
		return nil, errors.New("storage file factory unavailable")
	}

	operation := s.storageFiles.GetFileFromPathAsync(path)
	if operation == nil {
		return nil, fmt.Errorf("GetFileFromPathAsync returned nil for %q", path)
	}

	var asyncInfo *winrt.IAsyncInfo
	hr := operation.QueryInterface(&winrt.IID_IAsyncInfo, unsafe.Pointer(&asyncInfo))
	if win32.FAILED(hr) || asyncInfo == nil {
		return nil, fmt.Errorf("query IAsyncInfo for %q failed: %s", path, win32.HRESULT_ToString(hr))
	}
	com.AddToScope(asyncInfo)

	deadline := time.Now().Add(storageResolveTimeout)
	for {
		switch asyncInfo.Get_Status() {
		case winrt.AsyncStatus_Completed:
			file := operation.GetResults()
			if file == nil {
				return nil, fmt.Errorf("GetResults returned nil for %q", path)
			}
			return file, nil
		case winrt.AsyncStatus_Error:
			errorCode := asyncInfo.Get_ErrorCode()
			return nil, fmt.Errorf("GetFileFromPathAsync failed for %q: %s", path, win32.HRESULT_ToString(win32.HRESULT(errorCode.Value)))
		case winrt.AsyncStatus_Canceled:
			return nil, fmt.Errorf("GetFileFromPathAsync canceled for %q", path)
		default:
			if time.Now().After(deadline) {
				asyncInfo.Cancel()
				return nil, fmt.Errorf("GetFileFromPathAsync timed out for %q", path)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (s *smtcRuntimeState) applyTimeline(positionMS int64, durationMS int64) {
	if s.controls2 == nil || s.timeline == nil {
		return
	}
	s.timeline.Put_StartTime(millisecondsToTimeSpan(0))
	s.timeline.Put_MinSeekTime(millisecondsToTimeSpan(0))
	s.timeline.Put_Position(millisecondsToTimeSpan(positionMS))
	s.timeline.Put_EndTime(millisecondsToTimeSpan(durationMS))
	s.timeline.Put_MaxSeekTime(millisecondsToTimeSpan(durationMS))
	s.controls2.UpdateTimelineProperties(s.timeline)
}

func (s *smtcRuntimeState) onButtonPressed(
	_ *winrt.ISystemMediaTransportControls,
	args *winrt.ISystemMediaTransportControlsButtonPressedEventArgs,
) com.Error {
	if s.session == nil || args == nil {
		return com.OK
	}

	switch args.Get_Button() {
	case winrt.SystemMediaTransportControlsButton_Play:
		go s.runAction("play", func() error {
			_, err := s.session.Play(context.Background())
			return err
		})
	case winrt.SystemMediaTransportControlsButton_Pause:
		go s.runAction("pause", func() error {
			_, err := s.session.Pause(context.Background())
			return err
		})
	case winrt.SystemMediaTransportControlsButton_Next:
		go s.runAction("next", func() error {
			_, err := s.session.Next(context.Background())
			return err
		})
	case winrt.SystemMediaTransportControlsButton_Previous:
		go s.runAction("previous", func() error {
			_, err := s.session.Previous(context.Background())
			return err
		})
	}

	return com.OK
}

func (s *smtcRuntimeState) runAction(name string, action func() error) {
	if err := action(); err != nil {
		log.Printf("smtc %s action failed: %v", name, err)
	}
}

func (s *smtcRuntimeState) metadataChanged(item *playback.SessionItem, artworkPath string) bool {
	if item == nil {
		return s.hasTrack
	}
	if !s.hasTrack {
		return true
	}
	if s.lastRecordingID != strings.TrimSpace(item.RecordingID) {
		return true
	}
	if s.lastTitle != strings.TrimSpace(item.Title) {
		return true
	}
	if s.lastSubtitle != strings.TrimSpace(item.Subtitle) {
		return true
	}
	return s.lastArtworkPath != strings.TrimSpace(artworkPath)
}

func newTimelineProperties() *winrt.ISystemMediaTransportControlsTimelineProperties {
	hs := winrt.NewHStr(timelineClassName)
	defer hs.Dispose()

	var inspect *win32.IInspectable
	hr := win32.RoActivateInstance(hs.Ptr, &inspect)
	if win32.FAILED(hr) || inspect == nil {
		return nil
	}

	timeline := (*winrt.ISystemMediaTransportControlsTimelineProperties)(unsafe.Pointer(inspect))
	com.AddToScope(timeline)
	return timeline
}

func newStorageFileStatics() *winrt.IStorageFileStatics {
	hs := winrt.NewHStr(storageFileClassName)
	defer hs.Dispose()

	var storageFiles *winrt.IStorageFileStatics
	hr := win32.RoGetActivationFactory(hs.Ptr, &winrt.IID_IStorageFileStatics, unsafe.Pointer(&storageFiles))
	if win32.FAILED(hr) || storageFiles == nil {
		return nil
	}

	com.AddToScope(storageFiles)
	return storageFiles
}

func newRandomAccessStreamReferenceStatics() *winrt.IRandomAccessStreamReferenceStatics {
	hs := winrt.NewHStr(streamReferenceClassName)
	defer hs.Dispose()

	var streamRefs *winrt.IRandomAccessStreamReferenceStatics
	hr := win32.RoGetActivationFactory(hs.Ptr, &winrt.IID_IRandomAccessStreamReferenceStatics, unsafe.Pointer(&streamRefs))
	if win32.FAILED(hr) || streamRefs == nil {
		return nil
	}

	com.AddToScope(streamRefs)
	return streamRefs
}

func mapPlaybackStatus(status playback.Status) winrt.MediaPlaybackStatus {
	switch status {
	case playback.StatusPlaying:
		return winrt.MediaPlaybackStatus_Playing
	case playback.StatusPaused, playback.StatusPending:
		return winrt.MediaPlaybackStatus_Paused
	default:
		return winrt.MediaPlaybackStatus_Stopped
	}
}

func millisecondsToTimeSpan(milliseconds int64) winrt.TimeSpan {
	if milliseconds < 0 {
		milliseconds = 0
	}
	return winrt.TimeSpan{Duration: milliseconds * timespanTickPerMS}
}

func normalizeLabel(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func optionalInt64Value(value *int64) int64 {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}

func clampInt64(value int64, min int64, max int64) int64 {
	if value < min {
		return min
	}
	if max > 0 && value > max {
		return max
	}
	return value
}
