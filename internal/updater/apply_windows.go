//go:build windows

package updater

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These are flag names rather than command-line arguments. main registers
// them with flag.String and handles both before taking the single-instance
// mutex. They are deliberately undocumented implementation details.
const (
	ApplyHelperFlag = "internal-apply-update"
	ReadyMarkerFlag = "internal-update-ready"
)

const (
	helperReadyTimeout   = 10 * time.Second
	parentExitTimeout    = 2 * time.Minute
	newReadyTimeout      = 45 * time.Second
	processStopTimeout   = 5 * time.Second
	readyPollInterval    = 100 * time.Millisecond
	applyPhasePrepared   = "prepared"
	applyPhaseRolledBack = "rolled_back"
)

type applyTimeouts struct {
	helperReady time.Duration
	parentExit  time.Duration
	newReady    time.Duration
	processStop time.Duration
	readyPoll   time.Duration
}

var defaultApplyTimeouts = applyTimeouts{
	helperReady: helperReadyTimeout,
	parentExit:  parentExitTimeout,
	newReady:    newReadyTimeout,
	processStop: processStopTimeout,
	readyPoll:   readyPollInterval,
}

// applyPlan is the complete transaction journal consumed by the helper copy.
// Stage and backup live beside TargetPath so each rename stays on one volume.
type applyPlan struct {
	ParentPID       int    `json:"parent_pid"`
	TargetPath      string `json:"target_path"`
	ApplyMutexName  string `json:"apply_mutex_name"`
	Phase           string `json:"phase"`
	StagePath       string `json:"stage_path"`
	BackupPath      string `json:"backup_path"`
	HelperPath      string `json:"helper_path"`
	HelperReadyPath string `json:"helper_ready_path"`
	ReadyMarkerPath string `json:"ready_marker_path"`
	ConfigPath      string `json:"config_path,omitempty"`
	ExpectedVersion string `json:"expected_version"`
	ExpectedSHA256  string `json:"expected_sha256"`
}

type readyMarker struct {
	Version string    `json:"version"`
	PID     int       `json:"pid"`
	ReadyAt time.Time `json:"ready_at"`
}

// SystemApplyPreparer bridges Manager's provider-independent seam to the
// Windows self-replacement transaction.
type SystemApplyPreparer struct{}

// Prepare copies the already verified download out of Manager's temporary
// directory and launches the helper. The manager can remove its temporary
// directory as soon as this method returns.
func (SystemApplyPreparer) Prepare(update PreparedUpdate) error {
	ops := windowsApplyOps{}
	current, err := ops.Executable()
	if err != nil {
		return fmt.Errorf("locate running executable: %w", err)
	}
	if err := requireSameExecutable(current, update.ExecutablePath); err != nil {
		return err
	}
	_, err = scheduleApplyWith(ops, current, update.StagedPath, update.ConfigPath,
		update.Version, update.SHA256)
	return err
}

func scheduleApplyWith(ops applyOps, current, newExecutable, configPath, expectedVersion,
	expectedSHA256 string) (string, error) {
	return scheduleApplyWithTimeouts(ops, current, newExecutable, configPath, expectedVersion, expectedSHA256,
		defaultApplyTimeouts)
}

func scheduleApplyWithTimeouts(ops applyOps, current, newExecutable, configPath, expectedVersion,
	expectedSHA256 string, timeouts applyTimeouts) (string, error) {
	var err error
	current, err = absolutePath(current)
	if err != nil {
		return "", fmt.Errorf("resolve running executable: %w", err)
	}
	newExecutable, err = absolutePath(newExecutable)
	if err != nil {
		return "", fmt.Errorf("resolve staged executable: %w", err)
	}
	if cleanPathEqual(current, newExecutable) {
		return "", errors.New("staged executable is the running executable")
	}
	expectedVersion = strings.TrimSpace(expectedVersion)
	if expectedVersion == "" {
		return "", errors.New("expected update version is empty")
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if err := validateSHA256(expectedSHA256); err != nil {
		return "", fmt.Errorf("expected update digest: %w", err)
	}
	if configPath != "" {
		configPath, err = absolutePath(configPath)
		if err != nil {
			return "", fmt.Errorf("resolve configuration path: %w", err)
		}
	}

	applyLock, err := ops.AcquireApplyLock(current)
	if err != nil {
		return "", fmt.Errorf("acquire update transaction lock: %w", err)
	}
	retainApplyLock := false
	defer func() {
		if !retainApplyLock {
			_ = applyLock.Close()
		}
	}()

	token, err := ops.RandomToken()
	if err != nil {
		return "", fmt.Errorf("create update transaction id: %w", err)
	}
	if token == "" || strings.ContainsAny(token, `\/`) {
		return "", errors.New("invalid update transaction id")
	}

	dir := filepath.Dir(current)
	stem := strings.TrimSuffix(filepath.Base(current), filepath.Ext(current))
	prefix := "." + stem + "-update-" + token
	plan := applyPlan{
		ParentPID:       ops.PID(),
		TargetPath:      current,
		ApplyMutexName:  applyLock.Name(),
		Phase:           applyPhasePrepared,
		StagePath:       filepath.Join(dir, prefix+"-stage.exe"),
		BackupPath:      filepath.Join(dir, prefix+"-backup.exe"),
		HelperPath:      filepath.Join(dir, prefix+"-helper.exe"),
		HelperReadyPath: filepath.Join(dir, prefix+"-helper-ready.json"),
		ReadyMarkerPath: filepath.Join(dir, prefix+"-ready.json"),
		ConfigPath:      configPath,
		ExpectedVersion: expectedVersion,
		ExpectedSHA256:  expectedSHA256,
	}
	planPath := filepath.Join(dir, prefix+"-plan.json")
	if plan.ParentPID <= 0 {
		return "", errors.New("current process id is invalid")
	}

	created := make([]string, 0, 3)
	cleanup := func() {
		for index := len(created) - 1; index >= 0; index-- {
			_ = ops.Remove(created[index])
		}
	}
	if err := ops.CopyFile(current, plan.HelperPath); err != nil {
		return "", fmt.Errorf("copy update helper: %w", err)
	}
	created = append(created, plan.HelperPath)
	if err := ops.CopyFile(newExecutable, plan.StagePath); err != nil {
		cleanup()
		return "", fmt.Errorf("stage verified executable beside target: %w", err)
	}
	created = append(created, plan.StagePath)
	stageDigest, err := ops.FileSHA256(plan.StagePath)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("hash staged executable: %w", err)
	}
	if !strings.EqualFold(stageDigest, plan.ExpectedSHA256) {
		cleanup()
		return "", errors.New("staged executable changed after signature verification")
	}

	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		cleanup()
		return "", fmt.Errorf("encode update plan: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := ops.CreateFile(planPath, encoded); err != nil {
		cleanup()
		return "", fmt.Errorf("write update plan: %w", err)
	}
	created = append(created, planPath)

	process, err := ops.StartMonitored(plan.HelperPath, []string{"-" + ApplyHelperFlag, planPath})
	if err != nil {
		cleanup()
		return "", fmt.Errorf("start update helper: %w", err)
	}
	if err := waitForProcessMarker(ops, plan.HelperReadyPath, plan.ExpectedVersion, process,
		positiveDuration(timeouts.helperReady, helperReadyTimeout),
		positiveDuration(timeouts.readyPoll, readyPollInterval), "update helper"); err != nil {
		stopErr := stopMonitoredProcess(process, timeouts.processStop)
		// Never pull a plan out from under a helper that Windows could not
		// confirm as stopped. The parent remains alive, so such a helper can do
		// no replacement and its bounded parent wait eventually expires.
		if stopErr == nil {
			_ = ops.Remove(plan.HelperReadyPath)
			cleanup()
		} else {
			// The helper may still open or already hold the mutex. Preserve the
			// parent lease until process exit so no second transaction can stage
			// against the same target.
			retainApplyLock = true
		}
		return "", errors.Join(fmt.Errorf("update helper handshake: %w", err),
			wrapIfError("stop update helper", stopErr))
	}
	// The helper opened this same named mutex before acknowledging readiness.
	// Keep ownership until process exit; Windows then hands it to the waiting
	// helper as an abandoned mutex without a cross-session unlock window.
	retainApplyLock = true
	return planPath, nil
}

// RunApplyHelper executes a prepared plan. It must run before the helper tries
// to acquire the application's single-instance mutex. Every failure is also
// written beside the plan because a windowsgui process has no reliable stderr.
func RunApplyHelper(planPath string) error {
	return runAndPersistApplyHelperWith(windowsApplyOps{}, planPath, defaultApplyTimeouts)
}

func runAndPersistApplyHelperWith(ops applyOps, planPath string, timeouts applyTimeouts) error {
	err := runApplyHelperWith(ops, planPath, timeouts)
	if err == nil {
		return nil
	}

	message := time.Now().UTC().Format(time.RFC3339) + "\n" + err.Error() + "\n"
	if persistErr := ops.ReplaceFile(applyErrorPath(planPath), []byte(message)); persistErr != nil {
		return errors.Join(err, fmt.Errorf("persist update helper error: %w", persistErr))
	}
	return err
}

func applyErrorPath(planPath string) string { return planPath + ".error.txt" }

const (
	maxApplyErrorFiles = 16
	maxApplyErrorBytes = 64 << 10
)

var errApplyErrorTooLarge = errors.New("update helper error exceeds the size limit")

// ApplyError is a persisted failure reported by an update helper after it
// restarted the normal executable. Transaction is the random update token,
// not an arbitrary filename supplied by the filesystem.
type ApplyError struct {
	Transaction string
	Message     string
}

// ReadApplyErrors consumes bounded helper error reports belonging to the
// current executable. A report is removed only after its complete contents
// were read successfully; oversized or unreadable reports remain for audit.
func ReadApplyErrors() ([]ApplyError, error) {
	return readApplyErrorsWith(windowsApplyOps{})
}

func readApplyErrorsWith(ops applyOps) ([]ApplyError, error) {
	current, err := ops.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable for update errors: %w", err)
	}
	current, err = absolutePath(current)
	if err != nil {
		return nil, fmt.Errorf("resolve executable for update errors: %w", err)
	}
	stem := strings.TrimSuffix(filepath.Base(current), filepath.Ext(current))
	files, err := ops.ListDir(filepath.Dir(current))
	if err != nil {
		return nil, fmt.Errorf("list update helper errors: %w", err)
	}
	sort.Strings(files)

	results := make([]ApplyError, 0)
	var failures []error
	matched := 0
	for _, path := range files {
		transaction, ok := applyErrorArtifact(path, stem)
		if !ok {
			continue
		}
		matched++
		if matched > maxApplyErrorFiles {
			continue
		}
		data, err := ops.ReadFileLimit(path, maxApplyErrorBytes)
		if err != nil {
			failures = append(failures, fmt.Errorf("read update error %s: %w", transaction, err))
			continue
		}
		message := strings.TrimSpace(strings.ToValidUTF8(string(data), "�"))
		if message == "" {
			message = "update helper returned an empty error report"
		}
		results = append(results, ApplyError{Transaction: transaction, Message: message})
		if err := ops.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, fmt.Errorf("consume update error %s: %w", transaction, err))
		}
	}
	if matched > maxApplyErrorFiles {
		failures = append(failures, fmt.Errorf("update helper error limit exceeded: found %d, read %d",
			matched, maxApplyErrorFiles))
	}
	return results, errors.Join(failures...)
}

func applyErrorArtifact(path, stem string) (transaction string, ok bool) {
	base := filepath.Base(path)
	start := "." + stem + "-update-"
	suffix := "-plan.json.error.txt"
	lowerBase := strings.ToLower(base)
	if !strings.HasPrefix(lowerBase, strings.ToLower(start)) ||
		!strings.HasSuffix(lowerBase, suffix) {
		return "", false
	}
	token := base[len(start) : len(base)-len(suffix)]
	if !isUpdateToken(token) {
		return "", false
	}
	return strings.ToLower(token), true
}

// RecoverInterruptedApply schedules restoration of the previous executable
// when a durable plan and backup survived a process or machine crash. A true
// result means the recovery helper is running and the caller MUST exit at
// once so it can atomically restore and restart TargetPath. main must not call
// this for a process carrying ReadyMarkerFlag: that is the expected new
// process whose readiness the existing helper is currently evaluating.
func RecoverInterruptedApply() (mustExit bool, err error) {
	return recoverInterruptedApplyWith(windowsApplyOps{})
}

func recoverInterruptedApplyWith(ops applyOps) (bool, error) {
	current, err := ops.Executable()
	if err != nil {
		return false, fmt.Errorf("locate running executable for update recovery: %w", err)
	}
	current, err = absolutePath(current)
	if err != nil {
		return false, fmt.Errorf("resolve running executable for update recovery: %w", err)
	}
	applyLock, lockErr := ops.AcquireApplyLock(current)
	if lockErr != nil && !errors.Is(lockErr, ErrBusy) {
		return false, fmt.Errorf("acquire update recovery lock: %w", lockErr)
	}
	retainApplyLock := false
	if applyLock != nil {
		defer func() {
			if !retainApplyLock {
				_ = applyLock.Close()
			}
		}()
	}
	stem := strings.TrimSuffix(filepath.Base(current), filepath.Ext(current))
	files, err := ops.ListDir(filepath.Dir(current))
	if err != nil {
		return false, fmt.Errorf("list update recovery plans: %w", err)
	}

	var recoveryPath string
	var recoveryPlan applyPlan
	busyHandoff := false
	for _, path := range files {
		_, kind, ok := updateArtifact(path, stem)
		if !ok || kind != "plan" {
			continue
		}
		data, err := ops.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("read interrupted update plan: %w", err)
		}
		var plan applyPlan
		if err := json.Unmarshal(data, &plan); err != nil {
			return false, fmt.Errorf("decode interrupted update plan: %w", err)
		}
		if err := validateApplyPlanPaths(path, plan); err != nil {
			return false, fmt.Errorf("validate interrupted update plan: %w", err)
		}
		if !cleanPathEqual(plan.TargetPath, current) {
			continue
		}
		backupExists, err := ops.FileExists(plan.BackupPath)
		if err != nil {
			return false, fmt.Errorf("inspect interrupted update backup: %w", err)
		}
		if plan.Phase == applyPhaseRolledBack && !backupExists {
			if applyLock == nil {
				busyHandoff = true
				continue
			}
			if err := finishAbandonedPreparation(ops, path, plan); err != nil {
				return false, fmt.Errorf("clean completed rollback handoff: %w", err)
			}
			continue
		}
		active, err := activeApplyHelper(ops, plan)
		if err != nil {
			return false, fmt.Errorf("inspect active update helper: %w", err)
		}
		if active {
			// The caller must release both the executable and the application
			// mutex. The already-running helper retains ownership of the plan.
			return true, nil
		}
		committed, err := applyReachedReady(ops, plan)
		if err != nil {
			return false, fmt.Errorf("inspect completed update marker: %w", err)
		}
		if committed {
			if applyLock == nil {
				// The live helper owns cleanup. Do not race its commit path.
				busyHandoff = true
				continue
			}
			if err := finishCommittedApply(ops, path, plan); err != nil {
				return false, fmt.Errorf("finish committed update cleanup: %w", err)
			}
			continue
		}
		if !backupExists {
			if applyLock == nil {
				// A prepared transaction with no explicit handoff is still live:
				// the owner may be between taking the mutex and publishing artifacts.
				return true, nil
			}
			if err := finishAbandonedPreparation(ops, path, plan); err != nil {
				return false, fmt.Errorf("clean abandoned update preparation: %w", err)
			}
			continue
		}
		if recoveryPath != "" {
			return false, errors.New("more than one interrupted update has a rollback backup")
		}
		recoveryPath, recoveryPlan = path, plan
	}
	if recoveryPath == "" {
		if applyLock == nil && !busyHandoff {
			return true, nil
		}
		return false, nil
	}
	if applyLock == nil {
		// Another helper owns a transaction that has crossed the backup
		// boundary. The current target must release its executable immediately.
		return true, nil
	}
	helperExists, err := ops.FileExists(recoveryPlan.HelperPath)
	if err != nil {
		return false, fmt.Errorf("inspect update recovery helper: %w", err)
	}
	if !helperExists {
		return false, errors.New("interrupted update has no recovery helper")
	}
	// A marker from the crashed helper must not satisfy the new helper's
	// handshake. The target mutex is held here, so no live helper can race this
	// retirement.
	if err := ops.Remove(recoveryPlan.HelperReadyPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("remove stale recovery helper marker: %w", err)
	}

	// The helper must wait for this newly launched target, not the stale parent
	// PID recorded before the crash. Rewriting the journal is durable before
	// the helper is started.
	recoveryPlan.ParentPID = ops.PID()
	encoded, err := json.MarshalIndent(recoveryPlan, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode update recovery plan: %w", err)
	}
	if err := ops.ReplaceFile(recoveryPath, append(encoded, '\n')); err != nil {
		return false, fmt.Errorf("write update recovery plan: %w", err)
	}
	process, err := ops.StartMonitored(recoveryPlan.HelperPath,
		[]string{"-" + ApplyHelperFlag, recoveryPath})
	if err != nil {
		return false, fmt.Errorf("start update recovery helper: %w", err)
	}
	if err := waitForProcessMarker(ops, recoveryPlan.HelperReadyPath,
		recoveryPlan.ExpectedVersion, process, helperReadyTimeout,
		readyPollInterval, "update recovery helper"); err != nil {
		stopErr := stopMonitoredProcess(process, processStopTimeout)
		if stopErr == nil {
			_ = ops.Remove(recoveryPlan.HelperReadyPath)
		} else {
			retainApplyLock = true
		}
		return false, errors.Join(fmt.Errorf("update recovery helper handshake: %w", err),
			wrapIfError("stop update recovery helper", stopErr))
	}
	retainApplyLock = true
	return true, nil
}

func activeApplyHelper(ops applyOps, plan applyPlan) (bool, error) {
	data, err := ops.ReadFile(plan.HelperReadyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var marker readyMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false, nil
	}
	if marker.Version != plan.ExpectedVersion || marker.PID <= 0 {
		return false, nil
	}
	return ops.ProcessMatches(marker.PID, plan.HelperPath)
}

func applyReachedReady(ops applyOps, plan applyPlan) (bool, error) {
	data, err := ops.ReadFile(plan.ReadyMarkerPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var marker readyMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false, nil
	}
	if marker.Version != plan.ExpectedVersion || marker.PID <= 0 {
		return false, nil
	}
	digest, err := ops.FileSHA256(plan.TargetPath)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(digest, plan.ExpectedSHA256), nil
}

func finishCommittedApply(ops applyOps, planPath string, plan applyPlan) error {
	for _, path := range []string{
		plan.BackupPath,
		plan.StagePath,
		plan.ReadyMarkerPath,
		plan.HelperReadyPath,
		plan.HelperPath,
	} {
		if err := ops.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	// The journal is the commit record and must disappear last. A crash during
	// the best-effort removals above merely repeats this idempotent cleanup.
	if err := ops.Remove(planPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func finishCommittedApplyFromHelper(ops applyOps, planPath string, plan applyPlan) error {
	// Backup removal is the irreversible commit boundary. Until it succeeds,
	// both the ready marker and journal remain available so startup recovery can
	// prove the installed target and must never mistake it for a rollback.
	for _, path := range []string{
		plan.BackupPath,
		plan.StagePath,
		plan.ReadyMarkerPath,
		plan.HelperReadyPath,
	} {
		if err := ops.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	// A running helper cannot remove its own executable on Windows. Once the
	// journal is gone, CleanupApplyArtifacts removes that harmless leftover.
	if err := ops.Remove(planPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func finishAbandonedPreparation(ops applyOps, planPath string, plan applyPlan) error {
	for _, path := range []string{
		plan.StagePath,
		plan.ReadyMarkerPath,
		plan.HelperReadyPath,
	} {
		if err := ops.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	// A just-released helper can still have its executable mapped for a few
	// instructions. It is non-transactional once the plan is gone and the
	// regular artifact cleanup will retry it.
	_ = ops.Remove(plan.HelperPath)
	if err := ops.Remove(planPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func runApplyHelperWith(ops applyOps, planPath string, timeouts applyTimeouts) error {
	planPath, err := absolutePath(planPath)
	if err != nil {
		return fmt.Errorf("resolve update plan: %w", err)
	}
	data, err := ops.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read update plan: %w", err)
	}
	var plan applyPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("decode update plan: %w", err)
	}
	if err := validateApplyPlan(ops, planPath, plan); err != nil {
		return fmt.Errorf("validate update plan: %w", err)
	}
	applyLock, err := ops.OpenApplyLock(plan.ApplyMutexName)
	if err != nil {
		return fmt.Errorf("open update transaction lock: %w", err)
	}
	defer applyLock.Close()

	// A marker can only prove this launch if it did not predate the launch.
	if err := ops.Remove(plan.ReadyMarkerPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove stale ready marker: %w", err)
	}
	if err := writeProcessMarker(ops, plan.HelperReadyPath, plan.ExpectedVersion, ops.PID()); err != nil {
		return fmt.Errorf("signal update helper readiness: %w", err)
	}
	if err := ops.WaitForProcess(plan.ParentPID, timeouts.parentExit); err != nil {
		return fmt.Errorf("wait for parent process %d: %w", plan.ParentPID, err)
	}
	return applyLock.WithOwnership(positiveDuration(timeouts.parentExit, parentExitTimeout), func() error {
		return runLockedApplyTransaction(ops, planPath, plan, timeouts)
	})
}

func runLockedApplyTransaction(ops applyOps, planPath string, plan applyPlan, timeouts applyTimeouts) error {
	backupExists, err := ops.FileExists(plan.BackupPath)
	if err != nil {
		return fmt.Errorf("inspect update backup: %w", err)
	}
	if backupExists {
		if err := rollbackApply(ops, planPath, plan, nil, timeouts); err != nil {
			return fmt.Errorf("recover interrupted update: %w", err)
		}
		return nil
	}
	stageDigest, err := ops.FileSHA256(plan.StagePath)
	if err != nil {
		return restartUnchangedAfterApplyFailure(ops, planPath, plan,
			fmt.Errorf("hash staged executable before install: %w", err))
	}
	if !strings.EqualFold(stageDigest, plan.ExpectedSHA256) {
		return restartUnchangedAfterApplyFailure(ops, planPath, plan,
			errors.New("staged executable changed before install"))
	}

	// Copying first keeps TargetPath launchable even if the machine loses power
	// at the exact transaction boundary. The following same-volume replacement
	// is one durable filesystem operation, never a remove-then-rename gap.
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		return restartUnchangedAfterApplyFailure(ops, planPath, plan,
			fmt.Errorf("back up current executable: %w", err))
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		rollbackErr := rollbackFailure(ops, planPath, plan, nil, timeouts)
		return errors.Join(fmt.Errorf("install staged executable: %w", err), rollbackErr)
	}

	process, err := ops.StartMonitored(plan.TargetPath,
		restartArguments(plan.ConfigPath, plan.ReadyMarkerPath))
	if err != nil {
		rollbackErr := rollbackFailure(ops, planPath, plan, nil, timeouts)
		return errors.Join(fmt.Errorf("start updated executable: %w", err), rollbackErr)
	}
	if err := waitForReadyMarker(ops, plan, process, timeouts); err != nil {
		rollbackErr := rollbackFailure(ops, planPath, plan, process, timeouts)
		return errors.Join(err, rollbackErr)
	}

	// A healthy replacement is committed. Cleanup remains transactional: a
	// failure leaves enough evidence for startup recovery to finish the commit,
	// never to roll it back.
	if err := finishCommittedApplyFromHelper(ops, planPath, plan); err != nil {
		return fmt.Errorf("finish committed update cleanup: %w", err)
	}
	return nil
}

func validateApplyPlan(ops applyOps, planPath string, plan applyPlan) error {
	if plan.ParentPID <= 0 || plan.ParentPID == ops.PID() {
		return fmt.Errorf("invalid parent pid %d", plan.ParentPID)
	}
	if err := validateApplyPlanPaths(planPath, plan); err != nil {
		return err
	}

	self, err := ops.Executable()
	if err != nil {
		return fmt.Errorf("locate helper executable: %w", err)
	}
	if !cleanPathEqual(self, plan.HelperPath) {
		return fmt.Errorf("plan helper %q does not match running helper %q", plan.HelperPath, self)
	}
	return nil
}

func validateApplyPlanPaths(planPath string, plan applyPlan) error {
	if plan.ParentPID <= 0 {
		return fmt.Errorf("invalid parent pid %d", plan.ParentPID)
	}
	if strings.TrimSpace(plan.ExpectedVersion) == "" {
		return errors.New("expected version is empty")
	}
	if plan.Phase != applyPhasePrepared && plan.Phase != applyPhaseRolledBack {
		return fmt.Errorf("invalid update phase %q", plan.Phase)
	}
	if err := validateSHA256(plan.ExpectedSHA256); err != nil {
		return fmt.Errorf("expected SHA-256: %w", err)
	}

	paths := []struct {
		name string
		path string
	}{
		{"plan", planPath},
		{"target", plan.TargetPath},
		{"stage", plan.StagePath},
		{"backup", plan.BackupPath},
		{"helper", plan.HelperPath},
		{"helper ready marker", plan.HelperReadyPath},
		{"ready marker", plan.ReadyMarkerPath},
	}
	for index := range paths {
		resolved, err := absolutePath(paths[index].path)
		if err != nil {
			return fmt.Errorf("%s path: %w", paths[index].name, err)
		}
		paths[index].path = resolved
		for previous := 0; previous < index; previous++ {
			if cleanPathEqual(paths[previous].path, resolved) {
				return fmt.Errorf("%s path duplicates %s path", paths[index].name, paths[previous].name)
			}
		}
	}
	expectedMutexName, err := applyMutexName(paths[1].path)
	if err != nil {
		return fmt.Errorf("derive update mutex name: %w", err)
	}
	if plan.ApplyMutexName != expectedMutexName {
		return fmt.Errorf("update mutex name %q does not match target", plan.ApplyMutexName)
	}

	targetDir := filepath.Dir(paths[1].path)
	for _, entry := range paths[2:] {
		if !cleanPathEqual(filepath.Dir(entry.path), targetDir) {
			return fmt.Errorf("%s is not beside target", entry.name)
		}
	}
	if plan.ConfigPath != "" {
		if _, err := absolutePath(plan.ConfigPath); err != nil {
			return fmt.Errorf("configuration path: %w", err)
		}
	}

	return nil
}

func waitForReadyMarker(ops applyOps, plan applyPlan, process monitoredProcess, timeouts applyTimeouts) error {
	return waitForProcessMarker(ops, plan.ReadyMarkerPath, plan.ExpectedVersion, process,
		positiveDuration(timeouts.newReady, newReadyTimeout),
		positiveDuration(timeouts.readyPoll, readyPollInterval), "updated executable")
}

func waitForProcessMarker(ops applyOps, markerPath, expectedVersion string, process monitoredProcess,
	readyTimeout, poll time.Duration, description string) error {
	timer := time.NewTimer(readyTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		data, err := ops.ReadFile(markerPath)
		switch {
		case err == nil:
			var marker readyMarker
			if json.Unmarshal(data, &marker) == nil {
				if marker.Version != expectedVersion {
					return fmt.Errorf("%s reported version %q, want %q",
						description, marker.Version, expectedVersion)
				}
				if marker.PID != process.PID() {
					return fmt.Errorf("%s ready marker reported pid %d, want %d",
						description, marker.PID, process.PID())
				}
				return nil
			}
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("read update ready marker: %w", err)
		}

		select {
		case <-process.Done():
			if err := process.WaitErr(); err != nil {
				return fmt.Errorf("%s exited before becoming ready: %w", description, err)
			}
			return fmt.Errorf("%s exited before becoming ready", description)
		case <-timer.C:
			return fmt.Errorf("%s did not become ready within %s", description, readyTimeout)
		case <-ticker.C:
		}
	}
}

func restartUnchangedAfterApplyFailure(ops applyOps, planPath string, plan applyPlan, cause error) error {
	safeToRestart, cleanupErr := finishFailedApplyBeforeRestart(ops, planPath, plan)
	var restartErr error
	if safeToRestart {
		restartErr = ops.StartDetached(plan.TargetPath, restartArguments(plan.ConfigPath, ""))
	} else {
		restartErr = errors.New("unchanged executable was not restarted because the active helper journal could not be retired")
	}
	return errors.Join(cause,
		wrapIfError("finalize failed update", cleanupErr),
		wrapIfError("restart unchanged executable", restartErr))
}

// finishFailedApplyBeforeRestart publishes an explicit rollback handoff before
// the old executable is restarted. The journal remains until a later startup
// can acquire the mutex, so a process racing the helper's shutdown can prove
// that it is safe to continue despite the still-owned mutex.
func finishFailedApplyBeforeRestart(ops applyOps, planPath string, plan applyPlan) (bool, error) {
	var failures []error
	plan.Phase = applyPhaseRolledBack
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode rollback handoff: %w", err)
	}
	phaseRecorded := true
	if err := ops.ReplaceFile(planPath, append(encoded, '\n')); err != nil {
		phaseRecorded = false
		failures = append(failures, fmt.Errorf("write rollback handoff: %w", err))
	}

	for _, artifact := range []struct {
		path      string
		operation string
	}{
		{plan.StagePath, "remove failed update"},
		{plan.ReadyMarkerPath, "remove failed ready marker"},
	} {
		if err := ops.Remove(artifact.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, fmt.Errorf("%s: %w", artifact.operation, err))
		}
	}

	if err := ops.Remove(plan.HelperReadyPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		failures = append(failures, fmt.Errorf("remove helper ready marker: %w", err))
		// An invalid marker is just as safe as an absent marker: recovery cannot
		// associate it with the helper process that is still winding down.
		if replaceErr := ops.ReplaceFile(plan.HelperReadyPath, []byte("{}\n")); replaceErr == nil {
		} else {
			failures = append(failures, fmt.Errorf("retire helper ready marker: %w", replaceErr))
		}
	}
	return phaseRecorded, errors.Join(failures...)
}

func rollbackApply(ops applyOps, planPath string, plan applyPlan, process monitoredProcess,
	timeouts applyTimeouts) error {
	var failures []error
	if process != nil {
		if err := stopMonitoredProcess(process, timeouts.processStop); err != nil {
			failures = append(failures, fmt.Errorf("stop failed update process: %w", err))
			return fmt.Errorf("rollback update: %w", errors.Join(failures...))
		}
	}

	if err := ops.ReplacePath(plan.BackupPath, plan.TargetPath); err != nil {
		failures = append(failures, fmt.Errorf("restore previous executable: %w", err))
		return fmt.Errorf("rollback update: %w", errors.Join(failures...))
	}
	safeToRestart, cleanupErr := finishFailedApplyBeforeRestart(ops, planPath, plan)
	if cleanupErr != nil {
		failures = append(failures, cleanupErr)
	}
	if !safeToRestart {
		failures = append(failures,
			errors.New("previous executable was not restarted because the active helper journal could not be retired"))
	} else if err := ops.StartDetached(plan.TargetPath, restartArguments(plan.ConfigPath, "")); err != nil {
		failures = append(failures, fmt.Errorf("restart previous executable: %w", err))
	}
	if len(failures) > 0 {
		return fmt.Errorf("rollback update: %w", errors.Join(failures...))
	}
	return nil
}

func rollbackFailure(ops applyOps, planPath string, plan applyPlan, process monitoredProcess,
	timeouts applyTimeouts) error {
	if err := rollbackApply(ops, planPath, plan, process, timeouts); err != nil {
		return err
	}
	return errors.New("update rolled back to previous executable")
}

func stopMonitoredProcess(process monitoredProcess, timeout time.Duration) error {
	select {
	case <-process.Done():
		return nil
	default:
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	timer := time.NewTimer(positiveDuration(timeout, processStopTimeout))
	defer timer.Stop()
	select {
	case <-process.Done():
		return nil
	case <-timer.C:
		return errors.New("process did not exit after termination")
	}
}

func restartArguments(configPath, readyMarkerPath string) []string {
	args := []string{"-background"}
	if configPath != "" {
		args = append(args, "-config", configPath)
	}
	if readyMarkerPath != "" {
		args = append(args, "-"+ReadyMarkerFlag, readyMarkerPath)
	}
	return args
}

// MarkReady atomically tells the helper that the replacement executable
// completed normal startup. version must be the version embedded in this
// running binary, not a value copied from the update plan.
func MarkReady(markerPath, version string) error {
	markerPath, err := absolutePath(markerPath)
	if err != nil {
		return fmt.Errorf("resolve update ready marker: %w", err)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return errors.New("ready version is empty")
	}
	if err := writeProcessMarker(windowsApplyOps{}, markerPath, version, os.Getpid()); err != nil {
		return fmt.Errorf("write update ready marker: %w", err)
	}
	return nil
}

func writeProcessMarker(ops applyOps, markerPath, version string, pid int) error {
	data, err := json.Marshal(readyMarker{
		Version: version,
		PID:     pid,
		ReadyAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return ops.ReplaceFile(markerPath, append(data, '\n'))
}

// CleanupApplyArtifacts removes leftovers from completed update transactions.
// It is safe to call after every startup. If a helper is still running,
// Windows refuses to delete its executable and the whole matching transaction
// is left intact for the next call, including its rollback backup.
func CleanupApplyArtifacts() {
	cleanupApplyArtifactsWith(windowsApplyOps{})
}

func cleanupApplyArtifactsWith(ops applyOps) {
	current, err := ops.Executable()
	if err != nil {
		return
	}
	current, err = absolutePath(current)
	if err != nil {
		return
	}
	stem := strings.TrimSuffix(filepath.Base(current), filepath.Ext(current))
	matches, err := ops.ListDir(filepath.Dir(current))
	if err != nil {
		return
	}

	type artifacts struct {
		helper string
		plan   string
		paths  []string
	}
	transactions := map[string]*artifacts{}
	for _, path := range matches {
		prefix, kind, ok := updateArtifact(path, stem)
		if !ok {
			continue
		}
		transaction := transactions[prefix]
		if transaction == nil {
			transaction = &artifacts{}
			transactions[prefix] = transaction
		}
		if kind == "helper" {
			transaction.helper = path
			continue
		}
		if kind == "plan" {
			transaction.plan = path
			continue
		}
		transaction.paths = append(transaction.paths, path)
	}

	for _, transaction := range transactions {
		// A durable plan is the recovery journal. Only the helper that either
		// commits readiness or restores the backup may remove it.
		if transaction.plan != "" {
			continue
		}
		if transaction.helper != "" {
			if err := ops.Remove(transaction.helper); err != nil {
				continue
			}
		}
		for _, path := range transaction.paths {
			_ = ops.Remove(path)
		}
	}
}

func updateArtifact(path, stem string) (prefix, kind string, ok bool) {
	base := filepath.Base(path)
	start := "." + stem + "-update-"
	if !strings.HasPrefix(strings.ToLower(base), strings.ToLower(start)) {
		return "", "", false
	}
	suffixes := []struct {
		suffix string
		kind   string
	}{
		{"-helper.exe.partial", "partial"},
		{"-backup.exe.partial", "partial"},
		{"-stage.exe.partial", "partial"},
		{"-plan.json.partial", "partial"},
		{"-helper.exe", "helper"},
		{"-backup.exe", "backup"},
		{"-stage.exe", "stage"},
		{"-helper-ready.json", "helper-ready"},
		{"-ready.json", "ready"},
		{"-plan.json", "plan"},
	}
	for _, candidate := range suffixes {
		if !strings.HasSuffix(strings.ToLower(base), candidate.suffix) {
			continue
		}
		token := base[len(start) : len(base)-len(candidate.suffix)]
		if !isUpdateToken(token) {
			return "", "", false
		}
		return filepath.Join(filepath.Dir(path), start+strings.ToLower(token)), candidate.kind, true
	}
	return "", "", false
}

func isUpdateToken(token string) bool {
	if len(token) != 24 {
		return false
	}
	for _, char := range token {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

func requireSameExecutable(current, configured string) error {
	if strings.TrimSpace(configured) == "" {
		return errors.New("configured executable path is empty")
	}
	currentResolved, err := resolveExistingPath(current)
	if err != nil {
		return fmt.Errorf("resolve running executable: %w", err)
	}
	configuredResolved, err := resolveExistingPath(configured)
	if err != nil {
		return fmt.Errorf("resolve configured executable: %w", err)
	}
	if !cleanPathEqual(currentResolved, configuredResolved) {
		return fmt.Errorf("configured executable %q is not the running executable %q", configured, current)
	}
	return nil
}

func resolveExistingPath(path string) (string, error) {
	path, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func absolutePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func cleanPathEqual(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(aAbs), filepath.Clean(bAbs))
}

func applyMutexName(targetPath string) (string, error) {
	targetPath, err := absolutePath(targetPath)
	if err != nil {
		return "", err
	}
	canonical := strings.ToLower(filepath.Clean(targetPath))
	digest := sha256.Sum256([]byte(canonical))
	return `Global\rig-exporter-update-` + hex.EncodeToString(digest[:]), nil
}

func validateSHA256(value string) error {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return errors.New("must be a 64-character hexadecimal SHA-256")
	}
	return nil
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func wrapIfError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type monitoredProcess interface {
	PID() int
	Done() <-chan struct{}
	WaitErr() error
	Kill() error
}

type parentApplyLock interface {
	Name() string
	Close() error
}

type helperApplyLock interface {
	WithOwnership(timeout time.Duration, transaction func() error) error
	Close() error
}

type applyOps interface {
	Executable() (string, error)
	PID() int
	RandomToken() (string, error)
	AcquireApplyLock(targetPath string) (parentApplyLock, error)
	OpenApplyLock(name string) (helperApplyLock, error)
	CopyFile(source, target string) error
	CreateFile(path string, data []byte) error
	ReplaceFile(path string, data []byte) error
	ReadFile(path string) ([]byte, error)
	ReadFileLimit(path string, limit int64) ([]byte, error)
	ListDir(path string) ([]string, error)
	FileExists(path string) (bool, error)
	FileSHA256(path string) (string, error)
	ProcessMatches(pid int, executable string) (bool, error)
	ReplacePath(source, target string) error
	Remove(path string) error
	StartDetached(path string, args []string) error
	StartMonitored(path string, args []string) (monitoredProcess, error)
	WaitForProcess(pid int, timeout time.Duration) error
}

type windowsApplyOps struct{}

const applyMutexSDDL = `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x00100001;;;AU)`

type windowsParentApplyLock struct {
	name       string
	release    chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
	closeError error
}

func (lock *windowsParentApplyLock) Name() string { return lock.name }

func (lock *windowsParentApplyLock) Close() error {
	lock.closeOnce.Do(func() { close(lock.release) })
	<-lock.done
	return lock.closeError
}

type windowsParentApplyLockResult struct {
	handle windows.Handle
	err    error
}

func acquireWindowsParentApplyLock(targetPath string) (parentApplyLock, error) {
	name, err := applyMutexName(targetPath)
	if err != nil {
		return nil, err
	}
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(applyMutexSDDL)
	if err != nil {
		return nil, fmt.Errorf("create update mutex security descriptor: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}
	lock := &windowsParentApplyLock{
		name:    name,
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	ready := make(chan windowsParentApplyLockResult, 1)
	go func() {
		// Windows mutex ownership belongs to a thread, not a Go goroutine. This
		// thread remains pinned for the full parent lease and therefore becomes
		// abandoned atomically when the parent process exits.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		existing, openErr := windows.OpenMutex(
			windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE, false, namePointer)
		if openErr == nil {
			_ = windows.CloseHandle(existing)
			ready <- windowsParentApplyLockResult{err: fmt.Errorf("%w: %s", ErrBusy, name)}
			return
		}
		if errors.Is(openErr, windows.ERROR_ACCESS_DENIED) {
			ready <- windowsParentApplyLockResult{err: fmt.Errorf("%w: %s", ErrBusy, name)}
			return
		}
		if !errors.Is(openErr, windows.ERROR_FILE_NOT_FOUND) {
			ready <- windowsParentApplyLockResult{err: openErr}
			return
		}
		handle, createErr := windows.CreateMutex(&attributes, true, namePointer)
		if createErr != nil {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
			if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) ||
				errors.Is(createErr, windows.ERROR_ACCESS_DENIED) {
				createErr = fmt.Errorf("%w: %s", ErrBusy, name)
			}
			ready <- windowsParentApplyLockResult{err: createErr}
			return
		}
		ready <- windowsParentApplyLockResult{handle: handle}
		<-lock.release
		lock.closeError = errors.Join(
			wrapIfError("release update mutex", windows.ReleaseMutex(handle)),
			wrapIfError("close update mutex", windows.CloseHandle(handle)),
		)
		close(lock.done)
	}()
	result := <-ready
	if result.err != nil {
		return nil, result.err
	}
	return lock, nil
}

type windowsHelperApplyLock struct {
	handle windows.Handle
	name   string
	mu     sync.Mutex
	closed bool
}

func (lock *windowsHelperApplyLock) WithOwnership(timeout time.Duration, transaction func() error) error {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.closed {
		return errors.New("update mutex is closed")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	result, err := windows.WaitForSingleObject(lock.handle, windowsTimeout(timeout))
	if err != nil {
		return fmt.Errorf("wait for update mutex %q: %w", lock.name, err)
	}
	switch result {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("update mutex %q remained owned for %s", lock.name, timeout)
	default:
		return fmt.Errorf("WaitForSingleObject for update mutex returned %#x", result)
	}

	transactionErr := transaction()
	releaseErr := windows.ReleaseMutex(lock.handle)
	return errors.Join(transactionErr, wrapIfError("release update mutex", releaseErr))
}

func (lock *windowsHelperApplyLock) Close() error {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.closed {
		return nil
	}
	lock.closed = true
	return windows.CloseHandle(lock.handle)
}

func windowsTimeout(timeout time.Duration) uint32 {
	milliseconds := timeout.Milliseconds()
	if milliseconds <= 0 {
		milliseconds = 1
	}
	if milliseconds >= math.MaxUint32 {
		milliseconds = math.MaxUint32 - 1
	}
	return uint32(milliseconds)
}

func (windowsApplyOps) Executable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return absolutePath(path)
}

func (windowsApplyOps) PID() int { return os.Getpid() }

func (windowsApplyOps) RandomToken() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (windowsApplyOps) AcquireApplyLock(targetPath string) (parentApplyLock, error) {
	return acquireWindowsParentApplyLock(targetPath)
}

func (windowsApplyOps) OpenApplyLock(name string) (helperApplyLock, error) {
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE, false, namePointer)
	if err != nil {
		return nil, err
	}
	return &windowsHelperApplyLock{handle: handle, name: name}, nil
}

func (windowsApplyOps) CopyFile(source, target string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", source)
	}

	// Publish the destination name only after every byte and the file metadata
	// are durable. In particular, BackupPath is the recovery signal: exposing a
	// half-written backup across a power loss would make recovery replace a good
	// target with a truncated executable.
	partialPath := target + ".partial"
	out, err := os.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := out.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		_ = os.Remove(partialPath)
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	closed = true
	from, err := windows.UTF16PtrFromString(partialPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func (windowsApplyOps) CreateFile(path string, data []byte) (err error) {
	partialPath := path + ".partial"
	file, err := os.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		_ = os.Remove(partialPath)
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	closed = true
	from, err := windows.UTF16PtrFromString(partialPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func (windowsApplyOps) ReplaceFile(path string, data []byte) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".update-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func (windowsApplyOps) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (windowsApplyOps) ReadFileLimit(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > limit {
		return nil, errApplyErrorTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errApplyErrorTooLarge
	}
	return data, nil
}

func (windowsApplyOps) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(path, entry.Name()))
	}
	return files, nil
}

func (windowsApplyOps) FileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (windowsApplyOps) FileSHA256(path string) (string, error) { return fileSHA256(path) }

func (windowsApplyOps) ProcessMatches(pid int, executable string) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	handle, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)

	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	if result == windows.WAIT_OBJECT_0 {
		return false, nil
	}
	if result != uint32(windows.WAIT_TIMEOUT) {
		return false, fmt.Errorf("WaitForSingleObject returned %#x", result)
	}

	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return false, err
	}
	return cleanPathEqual(windows.UTF16ToString(buffer[:size]), executable), nil
}

func (windowsApplyOps) ReplacePath(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func (windowsApplyOps) Remove(path string) error { return os.Remove(path) }

func (windowsApplyOps) StartDetached(path string, args []string) error {
	command := hiddenCommand(path, args)
	if err := command.Start(); err != nil {
		return err
	}
	if err := command.Process.Release(); err != nil {
		_ = command.Process.Kill()
		return err
	}
	return nil
}

func (windowsApplyOps) StartMonitored(path string, args []string) (monitoredProcess, error) {
	command := hiddenCommand(path, args)
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := &commandProcess{command: command, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		process.mu.Lock()
		process.waitErr = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process, nil
}

func hiddenCommand(path string, args []string) *exec.Cmd {
	command := exec.Command(path, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command
}

func (windowsApplyOps) WaitForProcess(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil // the parent exited before the helper opened its handle
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	milliseconds := timeout.Milliseconds()
	if milliseconds <= 0 {
		milliseconds = 1
	}
	if milliseconds >= math.MaxUint32 {
		milliseconds = math.MaxUint32 - 1
	}
	result, err := windows.WaitForSingleObject(handle, uint32(milliseconds))
	if err != nil {
		return err
	}
	switch result {
	case windows.WAIT_OBJECT_0:
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("process did not exit within %s", timeout)
	default:
		return fmt.Errorf("WaitForSingleObject returned %#x", result)
	}
}

type commandProcess struct {
	command *exec.Cmd
	done    chan struct{}
	mu      sync.RWMutex
	waitErr error
}

func (p *commandProcess) Done() <-chan struct{} { return p.done }

func (p *commandProcess) PID() int { return p.command.Process.Pid }

func (p *commandProcess) WaitErr() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.waitErr
}

func (p *commandProcess) Kill() error { return p.command.Process.Kill() }

var _ applyPreparer = SystemApplyPreparer{}
