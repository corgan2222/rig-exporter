//go:build windows

package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestCopyFilePublishesOnlyACompleteDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.exe")
	target := filepath.Join(dir, "target.exe")
	if err := os.WriteFile(source, []byte("complete executable"), 0o600); err != nil {
		t.Fatal(err)
	}

	ops := windowsApplyOps{}
	if err := ops.CopyFile(source, target); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "complete executable" {
		t.Errorf("target = %q", got)
	}
	if _, err := os.Stat(target + ".partial"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("partial file remains after success: %v", err)
	}

	if err := ops.CopyFile(source, target); err == nil {
		t.Fatal("CopyFile replaced an existing destination")
	}
	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "complete executable" {
		t.Errorf("existing target changed after collision: %q", got)
	}
	if _, err := os.Stat(target + ".partial"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("partial file remains after collision: %v", err)
	}
}

func TestPlanWritesAreAtomicAndNeverClobberANewJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transaction-plan.json")
	ops := windowsApplyOps{}

	if err := ops.CreateFile(path, []byte("first")); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if err := ops.CreateFile(path, []byte("unexpected replacement")); err == nil {
		t.Fatal("CreateFile replaced an existing journal")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Errorf("journal after collision = %q", got)
	}
	if _, err := os.Stat(path + ".partial"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("partial journal remains after collision: %v", err)
	}

	if err := ops.ReplaceFile(path, []byte("second")); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("replaced journal = %q", got)
	}
}

func TestApplyMutexNameIsStableAcrossPathCase(t *testing.T) {
	first, err := applyMutexName(`C:\Apps\rig-exporter.exe`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := applyMutexName(`c:\apps\RIG-EXPORTER.EXE`)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("mutex names differ: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, `Global\rig-exporter-update-`) {
		t.Errorf("mutex name = %q", first)
	}
}

func TestWindowsApplyMutexHandsOffAndCanBeReacquired(t *testing.T) {
	ops := windowsApplyOps{}
	target := filepath.Join(t.TempDir(), "rig-exporter.exe")
	parent, err := ops.AcquireApplyLock(target)
	if err != nil {
		t.Fatalf("AcquireApplyLock: %v", err)
	}
	defer parent.Close()
	if competing, err := ops.AcquireApplyLock(target); !errors.Is(err, ErrBusy) {
		if competing != nil {
			_ = competing.Close()
		}
		t.Fatalf("competing lock error = %v, want ErrBusy", err)
	}
	helper, err := ops.OpenApplyLock(parent.Name())
	if err != nil {
		t.Fatalf("OpenApplyLock: %v", err)
	}
	defer helper.Close()
	if err := parent.Close(); err != nil {
		t.Fatalf("release parent lock: %v", err)
	}
	owned := false
	if err := helper.WithOwnership(time.Second, func() error {
		owned = true
		return nil
	}); err != nil {
		t.Fatalf("helper ownership: %v", err)
	}
	if !owned {
		t.Fatal("helper transaction did not run")
	}
	if err := helper.Close(); err != nil {
		t.Fatalf("close helper lock: %v", err)
	}
	reacquired, err := ops.AcquireApplyLock(target)
	if err != nil {
		t.Fatalf("reacquire lock: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatalf("close reacquired lock: %v", err)
	}
}

func TestScheduleApplyStagesEverythingBeforeLaunchingTheHelper(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	ops.pid = 4242
	ops.token = "abc123"
	ops.files[ops.executable] = []byte("old executable")
	verified := `C:\Temp\rig-exporter-1.6.4.exe`
	ops.files[verified] = []byte("verified executable")
	ops.onStartMonitored = func(_ string, args []string) {
		var startedPlan applyPlan
		if err := json.Unmarshal(ops.files[args[1]], &startedPlan); err != nil {
			t.Fatal(err)
		}
		marker, err := json.Marshal(readyMarker{
			Version: startedPlan.ExpectedVersion,
			PID:     ops.process.PID(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[startedPlan.HelperReadyPath] = marker
	}

	digest := sha256.Sum256([]byte("verified executable"))
	planPath, err := scheduleApplyWith(ops, ops.executable, verified,
		`C:\Users\Stefan\custom.json`, "1.6.4", hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("scheduleApplyWith: %v", err)
	}

	var plan applyPlan
	if err := json.Unmarshal(ops.files[planPath], &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.ParentPID != 4242 || plan.TargetPath != ops.executable ||
		plan.ConfigPath != `C:\Users\Stefan\custom.json` || plan.ExpectedVersion != "1.6.4" {
		t.Fatalf("plan = %#v", plan)
	}
	wantMutexName, err := applyMutexName(plan.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ApplyMutexName != wantMutexName {
		t.Errorf("apply mutex = %q, want %q", plan.ApplyMutexName, wantMutexName)
	}
	if got := string(ops.files[plan.HelperPath]); got != "old executable" {
		t.Errorf("helper contents = %q", got)
	}
	if got := string(ops.files[plan.StagePath]); got != "verified executable" {
		t.Errorf("stage contents = %q", got)
	}
	if !cleanPathEqual(filepath.Dir(plan.TargetPath), filepath.Dir(plan.StagePath)) {
		t.Errorf("stage %q is not beside target %q", plan.StagePath, plan.TargetPath)
	}

	wantArgs := []string{"-" + ApplyHelperFlag, planPath}
	if ops.monitoredPath != plan.HelperPath || !reflect.DeepEqual(ops.monitoredArgs, wantArgs) {
		t.Errorf("helper launch = %q %q, want %q %q",
			ops.monitoredPath, ops.monitoredArgs, plan.HelperPath, wantArgs)
	}
	assertEventOrder(t, ops.events,
		"acquire-apply-lock:"+plan.ApplyMutexName,
		"copy:"+ops.executable+"->"+plan.HelperPath,
		"copy:"+verified+"->"+plan.StagePath,
		"create:"+planPath,
		"start-monitored:"+plan.HelperPath+" "+strings.Join(wantArgs, " "),
	)
}

func TestScheduleApplyRequiresAHealthyMatchingHelperHandshake(t *testing.T) {
	tests := []struct {
		name       string
		process    *fakeMonitoredProcess
		writePID   func(*fakeMonitoredProcess) int
		wantError  string
		wantKilled bool
	}{
		{
			name:      "early exit",
			process:   newFakeExitedProcess(errors.New("helper crashed")),
			wantError: "exited before becoming ready",
		},
		{
			name:       "timeout",
			process:    newFakeRunningProcess(),
			wantError:  "did not become ready",
			wantKilled: true,
		},
		{
			name:       "different pid",
			process:    newFakeRunningProcess(),
			writePID:   func(process *fakeMonitoredProcess) int { return process.PID() + 1 },
			wantError:  "ready marker reported pid",
			wantKilled: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops := newFakeApplyOps()
			ops.executable = `C:\Apps\rig-exporter.exe`
			ops.pid = 4242
			ops.token = "abcdef123456abcdef123456"
			ops.process = test.process
			ops.files[ops.executable] = []byte("old executable")
			verified := `C:\Temp\rig-exporter-1.6.4.exe`
			ops.files[verified] = []byte("verified executable")
			var plan applyPlan
			var planPath string
			ops.onStartMonitored = func(_ string, args []string) {
				planPath = args[1]
				if err := json.Unmarshal(ops.files[planPath], &plan); err != nil {
					t.Fatal(err)
				}
				if test.writePID == nil {
					return
				}
				marker, err := json.Marshal(readyMarker{
					Version: plan.ExpectedVersion,
					PID:     test.writePID(test.process),
				})
				if err != nil {
					t.Fatal(err)
				}
				ops.files[plan.HelperReadyPath] = marker
			}

			digest := sha256.Sum256([]byte("verified executable"))
			_, err := scheduleApplyWithTimeouts(ops, ops.executable, verified, "", "1.6.4",
				hex.EncodeToString(digest[:]), fastApplyTimeouts())
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if test.process.killed != test.wantKilled {
				t.Errorf("helper killed = %t, want %t", test.process.killed, test.wantKilled)
			}
			for _, path := range []string{plan.HelperPath, plan.StagePath, plan.HelperReadyPath, planPath} {
				if _, exists := ops.files[path]; exists {
					t.Errorf("failed handshake artifact %q was not removed", path)
				}
			}
			if got := string(ops.files[ops.executable]); got != "old executable" {
				t.Errorf("running target changed after failed handshake: %q", got)
			}
		})
	}
}

func TestScheduleApplyRejectsASecondTransactionForTheSameTarget(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	ops.pid = 4242
	ops.token = "abcdef123456abcdef123456"
	ops.files[ops.executable] = []byte("old executable")
	verified := `C:\Temp\rig-exporter-1.6.4.exe`
	ops.files[verified] = []byte("verified executable")
	ops.onStartMonitored = func(_ string, args []string) {
		var plan applyPlan
		if err := json.Unmarshal(ops.files[args[1]], &plan); err != nil {
			t.Fatal(err)
		}
		marker, err := json.Marshal(readyMarker{Version: plan.ExpectedVersion, PID: ops.process.PID()})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.HelperReadyPath] = marker
	}
	digest := sha256Hex([]byte("verified executable"))
	if _, err := scheduleApplyWith(ops, ops.executable, verified, "", "1.6.4", digest); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	if _, err := scheduleApplyWith(ops, ops.executable, verified, "", "1.6.4", digest); !errors.Is(err, ErrBusy) {
		t.Fatalf("second schedule error = %v, want ErrBusy", err)
	}
}

func TestScheduleApplyRetainsTheLockWhenTheHelperCannotBeStopped(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	ops.pid = 4242
	ops.token = "abcdef123456abcdef123456"
	ops.files[ops.executable] = []byte("old executable")
	verified := `C:\Temp\rig-exporter-1.6.4.exe`
	ops.files[verified] = []byte("verified executable")
	ops.process.killErr = errors.New("termination denied")
	digest := sha256Hex([]byte("verified executable"))

	_, err := scheduleApplyWithTimeouts(ops, ops.executable, verified, "", "1.6.4", digest,
		fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "termination denied") {
		t.Fatalf("schedule error = %v, want failed helper stop", err)
	}
	if !ops.applyLockHeld {
		t.Fatal("apply lock was released while the helper may still be running")
	}
	if _, err := scheduleApplyWith(ops, ops.executable, verified, "", "1.6.4", digest); !errors.Is(err, ErrBusy) {
		t.Fatalf("second schedule error = %v, want ErrBusy", err)
	}
}

func TestApplyHelperSwapsThenStartsAndWaitsForTheNewExecutable(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.onStartMonitored = func(_ string, _ []string) {
		marker, err := json.Marshal(readyMarker{Version: plan.ExpectedVersion, PID: 5150})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.ReadyMarkerPath] = marker
	}

	if err := runApplyHelperWith(ops, planPath, fastApplyTimeouts()); err != nil {
		t.Fatalf("runApplyHelperWith: %v", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Errorf("target contents = %q", got)
	}
	for _, removed := range []string{plan.BackupPath, plan.StagePath, plan.ReadyMarkerPath, planPath} {
		if _, exists := ops.files[removed]; exists {
			t.Errorf("transaction file %q still exists", removed)
		}
	}
	wantArgs := restartArguments(plan.ConfigPath, plan.ReadyMarkerPath)
	if ops.monitoredPath != plan.TargetPath || !reflect.DeepEqual(ops.monitoredArgs, wantArgs) {
		t.Errorf("updated launch = %q %q, want %q %q",
			ops.monitoredPath, ops.monitoredArgs, plan.TargetPath, wantArgs)
	}
	assertEventOrder(t, ops.events,
		"open-apply-lock:"+plan.ApplyMutexName,
		"replace:"+plan.HelperReadyPath,
		fmt.Sprintf("wait-parent:%d", plan.ParentPID),
		"wait-apply-lock:"+plan.ApplyMutexName,
		"copy:"+plan.TargetPath+"->"+plan.BackupPath,
		"replace-path:"+plan.StagePath+"->"+plan.TargetPath,
		"start-monitored:"+plan.TargetPath+" "+strings.Join(wantArgs, " "),
		"remove:"+plan.BackupPath,
	)
}

func TestApplyHelperRejectsAReadyMarkerFromAnotherProcess(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.onStartMonitored = func(_ string, _ []string) {
		marker, err := json.Marshal(readyMarker{
			Version: plan.ExpectedVersion,
			PID:     ops.process.PID() + 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.ReadyMarkerPath] = marker
	}

	err := runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "ready marker reported pid") {
		t.Fatalf("error = %v, want ready marker pid mismatch", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after spoofed marker rollback = %q", got)
	}
}

func TestApplyHelperRollsBackAndRestartsWhenTheNewExecutableCannotStart(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.monitoredErr[plan.TargetPath] = errors.New("blocked by Windows")
	var restartedMustExit bool
	ops.onStartDetached = func(_ string, _ []string) {
		helperExecutable, helperPID := ops.executable, ops.pid
		ops.executable, ops.pid = plan.TargetPath, 7777
		var err error
		restartedMustExit, err = recoverInterruptedApplyWith(ops)
		ops.executable, ops.pid = helperExecutable, helperPID
		if err != nil {
			t.Fatalf("recovery during rollback handoff: %v", err)
		}
	}

	err := runAndPersistApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "start updated executable") {
		t.Fatalf("error = %v, want new executable start failure", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after rollback = %q", got)
	}
	if restartedMustExit {
		t.Error("restarted old executable exited during the rollback handoff")
	}
	if _, exists := ops.files[plan.BackupPath]; exists {
		t.Error("backup still exists after it was restored")
	}
	if _, exists := ops.files[plan.StagePath]; exists {
		t.Error("failed executable still exists after rollback")
	}
	wantOldArgs := restartArguments(plan.ConfigPath, "")
	if ops.detachedPath != plan.TargetPath || !reflect.DeepEqual(ops.detachedArgs, wantOldArgs) {
		t.Errorf("rollback launch = %q %q, want %q %q",
			ops.detachedPath, ops.detachedArgs, plan.TargetPath, wantOldArgs)
	}
	result := string(ops.files[applyErrorPath(planPath)])
	if !strings.Contains(result, "blocked by Windows") || !strings.Contains(result, "rolled back") {
		t.Errorf("persisted helper result = %q", result)
	}
	assertEventOrder(t, ops.events,
		"copy:"+plan.TargetPath+"->"+plan.BackupPath,
		"replace-path:"+plan.StagePath+"->"+plan.TargetPath,
		"replace-path:"+plan.BackupPath+"->"+plan.TargetPath,
		"start-detached:"+plan.TargetPath,
		"replace:"+applyErrorPath(planPath),
	)
}

func TestApplyHelperRestoresTheBackupWhenInstallingTheStageFails(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.replacePathErr[plan.StagePath+"->"+plan.TargetPath] = errors.New("rename denied")

	err := runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "install staged executable") {
		t.Fatalf("error = %v, want stage install failure", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after rollback = %q", got)
	}
	if _, exists := ops.files[plan.BackupPath]; exists {
		t.Error("backup still exists after it was restored")
	}
	if _, exists := ops.files[plan.StagePath]; exists {
		t.Error("uninstalled stage still exists after rollback")
	}
	assertEventOrder(t, ops.events,
		"copy:"+plan.TargetPath+"->"+plan.BackupPath,
		"replace-path:"+plan.StagePath+"->"+plan.TargetPath,
		"replace-path:"+plan.BackupPath+"->"+plan.TargetPath,
		"replace:"+planPath,
		"remove:"+plan.StagePath,
		"remove:"+plan.HelperReadyPath,
		"start-detached:"+plan.TargetPath,
	)
}

func TestApplyHelperRejectsAStageChangedAfterVerification(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.files[plan.StagePath] = []byte("locally replaced executable")

	err := runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "changed before install") {
		t.Fatalf("error = %v, want changed staged executable", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after rejected stage = %q", got)
	}
	if _, exists := ops.files[plan.BackupPath]; exists {
		t.Error("backup was created before the staged digest was checked")
	}
	if ops.detachedPath != plan.TargetPath {
		t.Errorf("unchanged target restart = %q, want %q", ops.detachedPath, plan.TargetPath)
	}
}

func TestApplyHelperRecoversACrashAfterBackupWithoutEverRemovingTarget(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Fatalf("target disappeared while creating backup: %q", got)
	}
	ops.events = nil // only assert recovery operations below

	if err := runApplyHelperWith(ops, planPath, fastApplyTimeouts()); err != nil {
		t.Fatalf("recover helper: %v", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("recovered target = %q", got)
	}
	if _, exists := ops.files[plan.StagePath]; exists {
		t.Error("abandoned staged executable was not removed")
	}
	var handoff applyPlan
	if err := json.Unmarshal(ops.files[planPath], &handoff); err != nil {
		t.Fatalf("decode rollback handoff: %v", err)
	}
	if handoff.Phase != applyPhaseRolledBack {
		t.Errorf("recovery phase = %q, want %q", handoff.Phase, applyPhaseRolledBack)
	}
	assertEventOrder(t, ops.events,
		fmt.Sprintf("wait-parent:%d", plan.ParentPID),
		"replace-path:"+plan.BackupPath+"->"+plan.TargetPath,
		"start-detached:"+plan.TargetPath,
	)
}

func TestApplyHelperRecoversACrashAfterAtomicInstallBeforeReady(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Fatalf("interrupted target = %q", got)
	}
	ops.events = nil

	if err := runApplyHelperWith(ops, planPath, fastApplyTimeouts()); err != nil {
		t.Fatalf("recover helper: %v", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after pre-ready recovery = %q", got)
	}
	if ops.detachedPath != plan.TargetPath {
		t.Errorf("restarted path = %q, want %q", ops.detachedPath, plan.TargetPath)
	}
}

func TestRecoverInterruptedApplyTellsTheCurrentTargetToExit(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	ops.executable = plan.TargetPath // the new target was launched after a reboot
	ops.pid = 7777
	ops.events = nil
	staleMarker, err := json.Marshal(readyMarker{Version: plan.ExpectedVersion, PID: 6161})
	if err != nil {
		t.Fatal(err)
	}
	ops.files[plan.HelperReadyPath] = staleMarker
	ops.onStartMonitored = func(_ string, _ []string) {
		if _, exists := ops.files[plan.HelperReadyPath]; exists {
			t.Error("stale helper marker still existed when recovery helper started")
		}
		marker, err := json.Marshal(readyMarker{
			Version: plan.ExpectedVersion,
			PID:     ops.process.PID(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.HelperReadyPath] = marker
	}

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if !mustExit {
		t.Fatal("mustExit = false, want the current target to release its executable")
	}
	var rewritten applyPlan
	if err := json.Unmarshal(ops.files[planPath], &rewritten); err != nil {
		t.Fatal(err)
	}
	if rewritten.ParentPID != 7777 {
		t.Errorf("recovery parent pid = %d, want 7777", rewritten.ParentPID)
	}
	wantArgs := []string{"-" + ApplyHelperFlag, planPath}
	if ops.monitoredPath != plan.HelperPath || !reflect.DeepEqual(ops.monitoredArgs, wantArgs) {
		t.Errorf("recovery launch = %q %q, want %q %q",
			ops.monitoredPath, ops.monitoredArgs, plan.HelperPath, wantArgs)
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Errorf("target changed before current process exited: %q", got)
	}
}

func TestRecoverInterruptedApplyCleansAPlanThatNeverChangedTarget(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.executable = plan.TargetPath
	ops.pid = 7777

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if mustExit {
		t.Error("mustExit = true for a transaction with no backup")
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target changed during abandoned preparation cleanup: %q", got)
	}
	for _, path := range []string{plan.StagePath, plan.HelperReadyPath, plan.HelperPath, planPath} {
		if _, exists := ops.files[path]; exists {
			t.Errorf("abandoned preparation artifact %q remains", path)
		}
	}
}

func TestRecoverInterruptedApplyExitsWhenTheTargetLockIsBusyWithoutAHandoff(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	ops.pid = 7777
	ops.applyLockHeld = true

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if !mustExit {
		t.Fatal("mustExit = false for a busy target without a rollback handoff")
	}
}

func TestRecoverInterruptedApplyAllowsAnExplicitRollbackHandoff(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.executable = plan.TargetPath
	ops.pid = 7777
	ops.applyLockHeld = true
	plan.Phase = applyPhaseRolledBack
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	ops.files[planPath] = encoded

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if mustExit {
		t.Fatal("mustExit = true for an explicit rollback restart handoff")
	}
	if _, exists := ops.files[planPath]; !exists {
		t.Fatal("busy rollback handoff was cleaned without owning its mutex")
	}
}

func TestRecoverInterruptedApplyLeavesAnActiveTransactionToItsOwner(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	ops.executable = plan.TargetPath
	ops.pid = 7777
	marker, err := json.Marshal(readyMarker{
		Version: plan.ExpectedVersion,
		PID:     6161,
	})
	if err != nil {
		t.Fatal(err)
	}
	ops.files[plan.HelperReadyPath] = marker
	ops.processPaths[6161] = plan.HelperPath

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if !mustExit {
		t.Fatal("mustExit = false while the original update helper is active")
	}
	if ops.monitoredPath != "" || ops.detachedPath != "" {
		t.Errorf("competing recovery helper was started: monitored=%q detached=%q",
			ops.monitoredPath, ops.detachedPath)
	}
	var unchanged applyPlan
	if err := json.Unmarshal(ops.files[planPath], &unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged.ParentPID != plan.ParentPID {
		t.Errorf("active plan parent changed from %d to %d", plan.ParentPID, unchanged.ParentPID)
	}
}

func TestRecoverInterruptedApplyCommitsAProvenReadyUpdate(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	ops.executable = plan.TargetPath
	ops.pid = 7777
	marker, err := json.Marshal(readyMarker{
		Version: plan.ExpectedVersion,
		PID:     5150,
	})
	if err != nil {
		t.Fatal(err)
	}
	ops.files[plan.ReadyMarkerPath] = marker

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recoverInterruptedApplyWith: %v", err)
	}
	if mustExit {
		t.Fatal("mustExit = true for an update that already proved readiness")
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Errorf("committed target = %q", got)
	}
	for _, path := range []string{
		plan.BackupPath, plan.ReadyMarkerPath, plan.HelperReadyPath,
		plan.HelperPath, planPath,
	} {
		if _, exists := ops.files[path]; exists {
			t.Errorf("committed transaction artifact %q remains", path)
		}
	}
	if ops.detachedPath != "" || ops.monitoredPath != "" {
		t.Error("a recovery process was started for a committed update")
	}
}

func TestCommitCleanupFailureCannotTurnAHealthyUpdateIntoARollback(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.onStartMonitored = func(_ string, _ []string) {
		marker, err := json.Marshal(readyMarker{Version: plan.ExpectedVersion, PID: ops.process.PID()})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.ReadyMarkerPath] = marker
	}
	ops.removeErr[plan.BackupPath] = errors.New("sharing violation")

	err := runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "finish committed update cleanup") {
		t.Fatalf("helper error = %v, want commit cleanup failure", err)
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Fatalf("healthy target after cleanup failure = %q", got)
	}
	for _, path := range []string{plan.BackupPath, plan.ReadyMarkerPath, planPath} {
		if _, exists := ops.files[path]; !exists {
			t.Errorf("commit recovery evidence %q was removed", path)
		}
	}

	delete(ops.removeErr, plan.BackupPath)
	ops.executable = plan.TargetPath
	ops.pid = 7777
	ops.detachedPath = ""
	ops.monitoredPath = ""
	mustExit, err := recoverInterruptedApplyWith(ops)
	if err != nil {
		t.Fatalf("recover committed cleanup: %v", err)
	}
	if mustExit {
		t.Fatal("mustExit = true while finishing a proven commit")
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Errorf("target was rolled back during commit recovery: %q", got)
	}
	if ops.detachedPath != "" || ops.monitoredPath != "" {
		t.Fatal("commit cleanup launched a rollback process")
	}
	if _, exists := ops.files[planPath]; exists {
		t.Fatal("commit journal remains after recovery cleanup")
	}
}

func TestRecoverInterruptedApplyKeepsTheCurrentProcessWhenItsHelperFails(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	ops.executable = plan.TargetPath
	ops.pid = 7777
	ops.process = newFakeExitedProcess(errors.New("recovery helper crashed"))

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err == nil || !strings.Contains(err.Error(), "recovery helper handshake") {
		t.Fatalf("error = %v, want failed recovery handshake", err)
	}
	if mustExit {
		t.Fatal("mustExit = true although no recovery helper took ownership")
	}
	if got := string(ops.files[plan.TargetPath]); got != "new executable" {
		t.Errorf("running target changed after failed recovery handshake: %q", got)
	}
	if _, exists := ops.files[planPath]; !exists {
		t.Fatal("recovery plan was removed after failed helper handshake")
	}
}

func TestRecoveryRetainsTheLockWhenItsHelperCannotBeStopped(t *testing.T) {
	ops, _, plan := newApplyTransaction(t)
	if err := ops.CopyFile(plan.TargetPath, plan.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := ops.ReplacePath(plan.StagePath, plan.TargetPath); err != nil {
		t.Fatal(err)
	}
	ops.executable = plan.TargetPath
	ops.pid = 7777
	ops.process.killErr = errors.New("termination denied")
	ops.onStartMonitored = func(_ string, _ []string) {
		marker, err := json.Marshal(readyMarker{
			Version: plan.ExpectedVersion,
			PID:     ops.process.PID() + 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ops.files[plan.HelperReadyPath] = marker
	}

	mustExit, err := recoverInterruptedApplyWith(ops)
	if err == nil || !strings.Contains(err.Error(), "termination denied") {
		t.Fatalf("recovery error = %v, want failed helper stop", err)
	}
	if mustExit {
		t.Fatal("mustExit = true although the recovery handshake failed")
	}
	if !ops.applyLockHeld {
		t.Fatal("recovery lock was released while its helper may still be running")
	}
}

func TestApplyHelperKillsAndRollsBackAnUpdateThatNeverBecomesReady(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	staleMarker, err := json.Marshal(readyMarker{Version: plan.ExpectedVersion, PID: 123})
	if err != nil {
		t.Fatal(err)
	}
	ops.files[plan.ReadyMarkerPath] = staleMarker

	err = runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("error = %v, want ready timeout", err)
	}
	if !ops.process.killed {
		t.Error("timed-out updated process was not killed")
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after timeout rollback = %q", got)
	}
	if ops.detachedPath != plan.TargetPath {
		t.Errorf("restarted path = %q, want %q", ops.detachedPath, plan.TargetPath)
	}
	assertEventOrder(t, ops.events,
		"start-monitored:"+plan.TargetPath+" "+strings.Join(
			restartArguments(plan.ConfigPath, plan.ReadyMarkerPath), " "),
		"replace-path:"+plan.BackupPath+"->"+plan.TargetPath,
		"start-detached:"+plan.TargetPath,
	)
}

func TestApplyHelperRollsBackWhenTheNewProcessExitsBeforeReady(t *testing.T) {
	ops, planPath, plan := newApplyTransaction(t)
	ops.process = newFakeExitedProcess(errors.New("exit status 9"))

	err := runApplyHelperWith(ops, planPath, fastApplyTimeouts())
	if err == nil || !strings.Contains(err.Error(), "exited before becoming ready") {
		t.Fatalf("error = %v, want early process exit", err)
	}
	if ops.process.killed {
		t.Error("an already exited process was killed")
	}
	if got := string(ops.files[plan.TargetPath]); got != "old executable" {
		t.Errorf("target after process failure rollback = %q", got)
	}
}

func TestReadApplyErrorsConsumesOnlyReportsForTheCurrentExecutable(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	token := "ABCDEF123456ABCDEF123456"
	matching := `C:\Apps\.rig-exporter-update-` + token + `-plan.json.error.txt`
	foreignStem := `C:\Apps\.other-exporter-update-abcdef123456abcdef123456-plan.json.error.txt`
	invalidToken := `C:\Apps\.rig-exporter-update-not-a-token-plan.json.error.txt`
	similarName := `C:\Apps\.rig-exporter-update-abcdef123456abcdef123456-plan.json.error.txt.bak`
	ops.files[matching] = []byte("  helper failed safely\n")
	ops.files[foreignStem] = []byte("foreign")
	ops.files[invalidToken] = []byte("invalid")
	ops.files[similarName] = []byte("similar")

	reports, err := readApplyErrorsWith(ops)
	if err != nil {
		t.Fatalf("readApplyErrorsWith: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %#v", reports)
	}
	if reports[0].Transaction != strings.ToLower(token) || reports[0].Message != "helper failed safely" {
		t.Errorf("report = %#v", reports[0])
	}
	if _, exists := ops.files[matching]; exists {
		t.Error("successfully read helper error was not consumed")
	}
	for _, path := range []string{foreignStem, invalidToken, similarName} {
		if _, exists := ops.files[path]; !exists {
			t.Errorf("unrelated file %q was consumed", path)
		}
	}
	assertEventOrder(t, ops.events, "read-limited:"+matching, "remove:"+matching)
}

func TestReadApplyErrorsLeavesOversizedReportsUnconsumed(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	path := `C:\Apps\.rig-exporter-update-abcdef123456abcdef123456-plan.json.error.txt`
	ops.files[path] = make([]byte, maxApplyErrorBytes+1)

	reports, err := readApplyErrorsWith(ops)
	if !errors.Is(err, errApplyErrorTooLarge) {
		t.Fatalf("error = %v, want errApplyErrorTooLarge", err)
	}
	if len(reports) != 0 {
		t.Fatalf("oversized reports = %#v", reports)
	}
	if _, exists := ops.files[path]; !exists {
		t.Fatal("oversized helper error was consumed without being read")
	}
}

func TestReadApplyErrorsReportsRemovalFailureAfterReading(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	path := `C:\Apps\.rig-exporter-update-abcdef123456abcdef123456-plan.json.error.txt`
	ops.files[path] = []byte("rollback completed")
	ops.removeErr[path] = errors.New("sharing violation")

	reports, err := readApplyErrorsWith(ops)
	if err == nil || !strings.Contains(err.Error(), "sharing violation") {
		t.Fatalf("error = %v, want removal failure", err)
	}
	if len(reports) != 1 || reports[0].Message != "rollback completed" {
		t.Fatalf("reports = %#v", reports)
	}
	if _, exists := ops.files[path]; !exists {
		t.Fatal("report disappeared despite removal failure")
	}
	assertEventOrder(t, ops.events, "read-limited:"+path, "remove:"+path)
}

func TestCleanupApplyArtifactsProtectsAnActiveTransactionAndRemovesItLater(t *testing.T) {
	ops := newFakeApplyOps()
	ops.executable = `C:\Apps\rig-exporter.exe`
	oldPrefix := `C:\Apps\.rig-exporter-update-111111111111111111111111`
	activePrefix := `C:\Apps\.rig-exporter-update-222222222222222222222222`
	old := []string{
		oldPrefix + "-helper.exe",
		oldPrefix + "-ready.json",
		oldPrefix + "-backup.exe",
		oldPrefix + "-stage.exe",
	}
	persistedError := oldPrefix + "-plan.json.error.txt"
	active := []string{
		activePrefix + "-helper.exe",
		activePrefix + "-plan.json",
		activePrefix + "-backup.exe",
	}
	for _, path := range append(append([]string{}, old...), active...) {
		ops.files[path] = []byte("artifact")
	}
	ops.files[persistedError] = []byte("failed update details")
	unrelated := `C:\Apps\.rig-exporter-update-release-notes.txt`
	ops.files[unrelated] = []byte("keep")
	ops.removeErr[active[0]] = errors.New("sharing violation")

	cleanupApplyArtifactsWith(ops)

	for _, path := range old {
		if _, exists := ops.files[path]; exists {
			t.Errorf("completed artifact %q was not removed", path)
		}
	}
	for _, path := range active {
		if _, exists := ops.files[path]; !exists {
			t.Errorf("active transaction artifact %q was removed", path)
		}
	}
	if _, exists := ops.files[unrelated]; !exists {
		t.Error("unrelated similarly named file was removed")
	}
	if _, exists := ops.files[persistedError]; !exists {
		t.Error("persisted helper error was removed")
	}

	delete(ops.removeErr, active[0])
	cleanupApplyArtifactsWith(ops)
	for _, path := range active {
		if _, exists := ops.files[path]; !exists {
			t.Errorf("planned transaction artifact %q was removed", path)
		}
	}

	delete(ops.files, active[1]) // the helper completed and removed its plan
	cleanupApplyArtifactsWith(ops)
	for _, path := range active {
		if _, exists := ops.files[path]; exists {
			t.Errorf("finished artifact %q was not removed on the next startup", path)
		}
	}
}

func newApplyTransaction(t *testing.T) (*fakeApplyOps, string, applyPlan) {
	t.Helper()
	prefix := `C:\Apps\.rig-exporter-update-aaaaaaaaaaaaaaaaaaaaaaaa`
	plan := applyPlan{
		ParentPID:       4242,
		TargetPath:      `C:\Apps\rig-exporter.exe`,
		Phase:           applyPhasePrepared,
		StagePath:       prefix + "-stage.exe",
		BackupPath:      prefix + "-backup.exe",
		HelperPath:      prefix + "-helper.exe",
		HelperReadyPath: prefix + "-helper-ready.json",
		ReadyMarkerPath: prefix + "-ready.json",
		ConfigPath:      `C:\Users\Stefan\custom.json`,
		ExpectedVersion: "1.6.4",
		ExpectedSHA256:  sha256Hex([]byte("new executable")),
	}
	mutexName, err := applyMutexName(plan.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	plan.ApplyMutexName = mutexName
	planPath := prefix + "-plan.json"
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	ops := newFakeApplyOps()
	ops.executable = plan.HelperPath
	ops.pid = 9000
	ops.files[planPath] = encoded
	ops.files[plan.TargetPath] = []byte("old executable")
	ops.files[plan.StagePath] = []byte("new executable")
	ops.files[plan.HelperPath] = []byte("old executable")
	return ops, planPath, plan
}

func fastApplyTimeouts() applyTimeouts {
	return applyTimeouts{
		helperReady: 10 * time.Millisecond,
		parentExit:  10 * time.Millisecond,
		newReady:    10 * time.Millisecond,
		processStop: 10 * time.Millisecond,
		readyPoll:   time.Millisecond,
	}
}

type fakeApplyOps struct {
	executable string
	pid        int
	token      string
	files      map[string][]byte
	events     []string

	waitParentErr  error
	replacePathErr map[string]error
	removeErr      map[string]error
	detachedErr    map[string]error
	monitoredErr   map[string]error
	processPaths   map[int]string
	// processMatchesErr is what OpenProcess does when the pid now belongs to
	// somebody we are not allowed to look at — a SYSTEM service that inherited
	// it. Not the same as "no such process", and that difference is the point.
	processMatchesErr error
	process           *fakeMonitoredProcess
	onStartMonitored  func(string, []string)
	onStartDetached   func(string, []string)
	applyLockHeld     bool
	applyLockOwned    bool
	applyLockName     string
	openApplyLockErr  error
	applyLockWaitErr  error

	detachedPath  string
	detachedArgs  []string
	monitoredPath string
	monitoredArgs []string
}

func newFakeApplyOps() *fakeApplyOps {
	return &fakeApplyOps{
		files:          map[string][]byte{},
		replacePathErr: map[string]error{},
		removeErr:      map[string]error{},
		detachedErr:    map[string]error{},
		monitoredErr:   map[string]error{},
		processPaths:   map[int]string{},
		process:        newFakeRunningProcess(),
	}
}

func (o *fakeApplyOps) Executable() (string, error) { return o.executable, nil }
func (o *fakeApplyOps) PID() int                    { return o.pid }
func (o *fakeApplyOps) RandomToken() (string, error) {
	return o.token, nil
}

type fakeParentApplyLock struct {
	ops    *fakeApplyOps
	name   string
	closed bool
}

func (lock *fakeParentApplyLock) Name() string { return lock.name }

func (lock *fakeParentApplyLock) Close() error {
	if lock.closed {
		return nil
	}
	lock.closed = true
	lock.ops.events = append(lock.ops.events, "release-apply-lock:"+lock.name)
	lock.ops.applyLockHeld = false
	return nil
}

type fakeHelperApplyLock struct {
	ops    *fakeApplyOps
	name   string
	closed bool
}

func (lock *fakeHelperApplyLock) WithOwnership(_ time.Duration, transaction func() error) error {
	lock.ops.events = append(lock.ops.events, "wait-apply-lock:"+lock.name)
	if lock.ops.applyLockWaitErr != nil {
		return lock.ops.applyLockWaitErr
	}
	lock.ops.applyLockHeld = false // the fake parent exited before this wait
	lock.ops.applyLockOwned = true
	defer func() { lock.ops.applyLockOwned = false }()
	return transaction()
}

func (lock *fakeHelperApplyLock) Close() error {
	if lock.closed {
		return nil
	}
	lock.closed = true
	lock.ops.events = append(lock.ops.events, "close-apply-lock:"+lock.name)
	return nil
}

func (o *fakeApplyOps) AcquireApplyLock(targetPath string) (parentApplyLock, error) {
	name, err := applyMutexName(targetPath)
	if err != nil {
		return nil, err
	}
	o.events = append(o.events, "acquire-apply-lock:"+name)
	if o.applyLockHeld || o.applyLockOwned {
		return nil, ErrBusy
	}
	o.applyLockHeld = true
	o.applyLockName = name
	return &fakeParentApplyLock{ops: o, name: name}, nil
}

func (o *fakeApplyOps) OpenApplyLock(name string) (helperApplyLock, error) {
	o.events = append(o.events, "open-apply-lock:"+name)
	if o.openApplyLockErr != nil {
		return nil, o.openApplyLockErr
	}
	o.applyLockName = name
	return &fakeHelperApplyLock{ops: o, name: name}, nil
}

func (o *fakeApplyOps) CopyFile(source, target string) error {
	o.events = append(o.events, "copy:"+source+"->"+target)
	data, ok := o.files[source]
	if !ok {
		return fs.ErrNotExist
	}
	if _, exists := o.files[target]; exists {
		return fs.ErrExist
	}
	o.files[target] = append([]byte(nil), data...)
	return nil
}

func (o *fakeApplyOps) CreateFile(path string, data []byte) error {
	o.events = append(o.events, "create:"+path)
	if _, exists := o.files[path]; exists {
		return fs.ErrExist
	}
	o.files[path] = append([]byte(nil), data...)
	return nil
}

func (o *fakeApplyOps) ReplaceFile(path string, data []byte) error {
	o.events = append(o.events, "replace:"+path)
	o.files[path] = append([]byte(nil), data...)
	return nil
}

func (o *fakeApplyOps) ReadFile(path string) ([]byte, error) {
	data, ok := o.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (o *fakeApplyOps) ReadFileLimit(path string, limit int64) ([]byte, error) {
	o.events = append(o.events, "read-limited:"+path)
	data, ok := o.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if int64(len(data)) > limit {
		return nil, errApplyErrorTooLarge
	}
	return append([]byte(nil), data...), nil
}

func (o *fakeApplyOps) ListDir(dir string) ([]string, error) {
	var files []string
	for path := range o.files {
		if cleanPathEqual(filepath.Dir(path), dir) {
			files = append(files, path)
		}
	}
	return files, nil
}

func (o *fakeApplyOps) FileExists(path string) (bool, error) {
	_, exists := o.files[path]
	return exists, nil
}

func (o *fakeApplyOps) FileSHA256(path string) (string, error) {
	data, exists := o.files[path]
	if !exists {
		return "", fs.ErrNotExist
	}
	return sha256Hex(data), nil
}

func (o *fakeApplyOps) ProcessMatches(pid int, executable string) (bool, error) {
	if o.processMatchesErr != nil {
		return false, o.processMatchesErr
	}
	return cleanPathEqual(o.processPaths[pid], executable), nil
}

func (o *fakeApplyOps) ReplacePath(source, target string) error {
	o.events = append(o.events, "replace-path:"+source+"->"+target)
	if err := o.replacePathErr[source+"->"+target]; err != nil {
		return err
	}
	data, ok := o.files[source]
	if !ok {
		return fs.ErrNotExist
	}
	o.files[target] = data
	delete(o.files, source)
	return nil
}

func (o *fakeApplyOps) Remove(path string) error {
	o.events = append(o.events, "remove:"+path)
	if err := o.removeErr[path]; err != nil {
		return err
	}
	if _, exists := o.files[path]; !exists {
		return fs.ErrNotExist
	}
	delete(o.files, path)
	return nil
}

func (o *fakeApplyOps) StartDetached(path string, args []string) error {
	o.events = append(o.events, "start-detached:"+path)
	o.detachedPath = path
	o.detachedArgs = append([]string(nil), args...)
	if o.onStartDetached != nil {
		o.onStartDetached(path, args)
	}
	return o.detachedErr[path]
}

func (o *fakeApplyOps) StartMonitored(path string, args []string) (monitoredProcess, error) {
	o.events = append(o.events, "start-monitored:"+path+" "+strings.Join(args, " "))
	if err := o.monitoredErr[path]; err != nil {
		return nil, err
	}
	o.monitoredPath = path
	o.monitoredArgs = append([]string(nil), args...)
	select {
	case <-o.process.Done():
	default:
		o.processPaths[o.process.PID()] = path
	}
	if o.onStartMonitored != nil {
		o.onStartMonitored(path, args)
	}
	return o.process, nil
}

func (o *fakeApplyOps) WaitForProcess(pid int, _ time.Duration) error {
	o.events = append(o.events, fmt.Sprintf("wait-parent:%d", pid))
	return o.waitParentErr
}

type fakeMonitoredProcess struct {
	done    chan struct{}
	err     error
	killErr error
	killed  bool
	pid     int
}

func newFakeRunningProcess() *fakeMonitoredProcess {
	return &fakeMonitoredProcess{done: make(chan struct{}), pid: 5150}
}

func newFakeExitedProcess(err error) *fakeMonitoredProcess {
	p := newFakeRunningProcess()
	p.err = err
	close(p.done)
	return p
}

func (p *fakeMonitoredProcess) Done() <-chan struct{} { return p.done }
func (p *fakeMonitoredProcess) PID() int              { return p.pid }
func (p *fakeMonitoredProcess) WaitErr() error        { return p.err }
func (p *fakeMonitoredProcess) Kill() error {
	if p.killErr != nil {
		return p.killErr
	}
	p.killed = true
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	return nil
}

func assertEventOrder(t *testing.T, events []string, ordered ...string) {
	t.Helper()
	next := 0
	for _, event := range events {
		if next < len(ordered) && event == ordered[next] {
			next++
		}
	}
	if next != len(ordered) {
		t.Fatalf("events =\n  %s\nwant ordered events =\n  %s",
			strings.Join(events, "\n  "), strings.Join(ordered, "\n  "))
	}
}

// A file named prefix+suffix with nothing in between must be refused, not
// crash the program.
//
// The prefix ends with the same hyphen every suffix begins with, so such a
// name satisfies HasPrefix and HasSuffix at once while sharing that hyphen
// between them — and the token slice then asks for base[21:20].
//
// This program never writes such a name: prepareApply builds every one as
// prefix+token, and a token that is empty or carries a separator is rejected
// before it is used. The file has to arrive from somewhere else — left behind
// by an older build, copied by hand, or written by anyone who can put a file
// next to the executable.
//
// What makes it worth a guard is where the name is read. RecoverInterruptedApply
// runs on every start before anything else comes up, and ReadApplyErrors runs a
// couple of seconds after the tray appears. Nothing in this program recovers
// from a panic, and under -H windowsgui there is no console to print it to, so
// the process would vanish without a window and without a log line — and the
// next start would find the same file and do it again.
func TestArtifactNamesWithNoTokenAreRefused(t *testing.T) {
	const stem = "rig-exporter"

	// Every suffix updateArtifact knows, with no token in front of it.
	for _, name := range []string{
		".rig-exporter-update-helper.exe.partial",
		".rig-exporter-update-backup.exe.partial",
		".rig-exporter-update-stage.exe.partial",
		".rig-exporter-update-plan.json.partial",
		".rig-exporter-update-helper.exe",
		".rig-exporter-update-backup.exe",
		".rig-exporter-update-stage.exe",
		".rig-exporter-update-helper-ready.json",
		".rig-exporter-update-ready.json",
		".rig-exporter-update-plan.json",
	} {
		t.Run("updateArtifact/"+name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("updateArtifact(%q) panicked: %v", name, recovered)
				}
			}()
			if _, _, ok := updateArtifact(name, stem); ok {
				t.Errorf("updateArtifact(%q) accepted a name carrying no token", name)
			}
		})
	}

	t.Run("applyErrorArtifact", func(t *testing.T) {
		const name = ".rig-exporter-update-plan.json.error.txt"
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("applyErrorArtifact(%q) panicked: %v", name, recovered)
			}
		}()
		if _, ok := applyErrorArtifact(name, stem); ok {
			t.Errorf("applyErrorArtifact(%q) accepted a name carrying no token", name)
		}
	})

	// The well-formed names must still be accepted, or the guard has simply
	// turned the feature off.
	const token = "0123456789abcdef01234567"
	if _, kind, ok := updateArtifact(".rig-exporter-update-"+token+"-plan.json", stem); !ok || kind != "plan" {
		t.Errorf("updateArtifact rejected a well-formed plan name: ok=%v kind=%q", ok, kind)
	}
	if got, ok := applyErrorArtifact(".rig-exporter-update-"+token+"-plan.json.error.txt", stem); !ok || got != token {
		t.Errorf("applyErrorArtifact rejected a well-formed name: ok=%v token=%q", ok, got)
	}
}
