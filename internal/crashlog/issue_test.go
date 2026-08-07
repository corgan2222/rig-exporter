package crashlog

import (
	neturl "net/url"
	"strings"
	"testing"
	"time"
)

func reportWithLog() Report {
	return Report{
		Kind: KindPanic, Version: "1.9.2", Build: "170.abc",
		At: time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC),
		Text: "rig-exporter session 2026-08-07T15:00:00+02:00 version=1.9.2 build=170.abc pid=1\n" +
			"panic: assignment to entry in nil map\n\ngoroutine 1 [running]:\nmain.main()\n" +
			"\n\n" + logMarker + "\n" +
			`time=2026-08-07T15:00:01+02:00 level=INFO msg=starting` + "\n",
	}
}

func fieldsOf(t *testing.T, raw string) neturl.Values {
	t.Helper()
	parsed, err := neturl.Parse(raw)
	if err != nil {
		t.Fatalf("the prepared link is not a URL: %v", err)
	}
	return parsed.Query()
}

// The form is filled by id, not by label. These ids are the contract: a
// renamed one arrives empty and nothing says so.
func TestTheFormIsFilledByItsAgreedFieldNames(t *testing.T) {
	m := Machine{
		OS: "Windows 11 Pro 24H2 (26100.2314)", CPU: "AMD Ryzen 9 5950X",
		GPU: "NVIDIA GeForce RTX 5070 Ti", Elevated: true,
		Sources: "MSI Afterburner + NVIDIA NVML",
	}

	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), m))

	want := map[string]string{
		"template": "crash-report.yml",
		"build":    "1.9.2+170.abc",
		"windows":  "Windows 11 Pro 24H2 (26100.2314)",
		"platform": "real",
		"hardware": "AMD Ryzen 9 5950X · NVIDIA GeForce RTX 5070 Ti",
		"elevated": "yes",
		"sources":  "MSI Afterburner + NVIDIA NVML",
	}
	for id, value := range want {
		if got := q.Get(id); got != value {
			t.Errorf("%s = %q, want %q", id, got, value)
		}
	}
	if !strings.Contains(q.Get("panic"), "assignment to entry in nil map") {
		t.Errorf("panic = %q", q.Get("panic"))
	}
	if !strings.Contains(q.Get("log"), "msg=starting") {
		t.Errorf("log = %q", q.Get("log"))
	}
}

// The stack and the log go into different fields. Sending the whole record in
// one of them would put the log inside the field labelled "stack".
func TestTheStackAndTheLogGoIntoSeparateFields(t *testing.T) {
	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))

	if strings.Contains(q.Get("panic"), "msg=starting") {
		t.Error("the log ended up in the stack field")
	}
	if strings.Contains(q.Get("log"), "goroutine 1 [running]") {
		t.Error("the stack ended up in the log field")
	}
}

// Both long fields declare render: text, so GitHub wraps them in a code block
// itself. Ours would show up as literal backticks inside that block.
func TestTheLongFieldsCarryNoFencesOfTheirOwn(t *testing.T) {
	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))

	for _, id := range []string{"panic", "log"} {
		if strings.Contains(q.Get(id), "```") {
			t.Errorf("%s carries its own code fence", id)
		}
	}
}

// A label named in a URL that does not exist in the repository is discarded by
// GitHub without a word. The form declares its own.
func TestNoLabelIsSetInTheLink(t *testing.T) {
	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))

	if q.Has("labels") {
		t.Errorf("the link still sets labels = %q", q.Get("labels"))
	}
}

// The two fields a person owns stay empty, or the form answers its own
// questions and the answers are worthless.
func TestTheFieldsThatBelongToAPersonAreLeftEmpty(t *testing.T) {
	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))

	for _, id := range []string{"steps", "full"} {
		if q.Has(id) {
			t.Errorf("%s was filled in: %q", id, q.Get(id))
		}
	}
}

func TestAVirtualMachineSaysSoAndNamesItsHypervisor(t *testing.T) {
	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter",
		reportWithLog(), Machine{Hypervisor: "QEMU/KVM"}))

	if got := q.Get("platform"); got != "virtual (QEMU/KVM)" {
		t.Errorf("platform = %q", got)
	}
}

// Measured against the real form: 4172 characters of raw text became a
// 5604-character URL. However long the dump, the link has to stay openable.
func TestThePreparedLinkStaysOpenable(t *testing.T) {
	r := reportWithLog()
	r.Text = strings.Repeat("goroutine 99 [running]:\n", 4000) + logMarker +
		strings.Repeat("time=… level=INFO msg=x\n", 2000)

	raw := IssueURL("https://github.com/corgan2222/rig-exporter", r, Machine{})
	if len(raw) > 8000 {
		t.Errorf("the prepared link is %d characters; browsers start refusing near 8000", len(raw))
	}
}

// A broker address is a free text field. Somebody who types credentials into it
// gets them written to the log on every connect, and no key=value filter can
// see them there.
func TestCredentialsInsideAURLAreRemoved(t *testing.T) {
	r := reportWithLog()
	r.Text = "panic: boom\n" + logMarker + "\n" +
		`time=… level=INFO msg="mqtt connecting" broker=tcp://stefan:hunter2@broker.local:1883` + "\n"

	q := fieldsOf(t, IssueURL("https://github.com/corgan2222/rig-exporter", r, Machine{}))

	log := q.Get("log")
	if strings.Contains(log, "hunter2") || strings.Contains(log, "stefan") {
		t.Errorf("the credentials survived: %s", log)
	}
	// The host is where the fault often is, and stays.
	if !strings.Contains(log, "broker.local:1883") {
		t.Errorf("the host was removed along with them: %s", log)
	}
}
