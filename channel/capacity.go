package channel

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	agentEstimatedMemoryBytes = uint64(256 * 1024 * 1024)
	agentMinFreeMemoryBytes   = uint64(256 * 1024 * 1024)
	agentMaxLoadPerCPU        = 3.0
)

type agentCapacitySnapshot struct {
	CPUs                     int
	Load1                    float64
	LoadKnown                bool
	MemoryAvailableBytes     uint64
	MemoryKnown              bool
	MeasuredAgentMemoryBytes uint64
	MeasuredAgentSamples     int
}

type agentCapacityStatus struct {
	ActiveAgents         int
	TempAgents           int
	CPUs                 int
	Load1                float64
	LoadKnown            bool
	MemoryFree           uint64
	MemoryKnown          bool
	FallbackMax          int
	EstimatedAgentMemory uint64
	MeasuredAgentSamples int
	Allowed              bool
	Reason               string
}

type AgentCapacityError struct {
	Status    agentCapacityStatus
	Reclaimed int
}

func (e *AgentCapacityError) Error() string {
	if e == nil {
		return "agent capacity unavailable"
	}
	return L.Getf("error.agent_capacity", e.Reclaimed, e.Status.ActiveAgents, e.Status.TempAgents, e.Status.memoryText(), e.Status.cpuText())
}

func (e *AgentCapacityError) UserFacingError() string {
	return e.Error()
}

func defaultAgentCapacitySnapshot() agentCapacitySnapshot {
	cpus := runtime.NumCPU()
	if cpus <= 0 {
		cpus = 1
	}
	s := agentCapacitySnapshot{CPUs: cpus}
	if load, ok := readProcLoad1(); ok {
		s.Load1 = load
		s.LoadKnown = true
	} else if load, ok := readDarwinLoad1(); ok {
		s.Load1 = load
		s.LoadKnown = true
	}
	if avail, ok := readCgroupMemoryAvailable(); ok {
		s.MemoryAvailableBytes = avail
		s.MemoryKnown = true
	} else if avail, ok := readProcMemAvailable(); ok {
		s.MemoryAvailableBytes = avail
		s.MemoryKnown = true
	} else if avail, ok := readDarwinMemoryAvailable(); ok {
		s.MemoryAvailableBytes = avail
		s.MemoryKnown = true
	}
	return s
}

func readProcLoad1() (float64, bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) {
		return 0, false
	}
	return load, true
}

func readProcMemAvailable() (uint64, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

func readDarwinLoad1() (float64, bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	raw, err := exec.Command("/usr/sbin/sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0, false
	}
	return parseDarwinLoad1(string(raw))
}

func parseDarwinLoad1(raw string) (float64, bool) {
	raw = strings.NewReplacer("{", " ", "}", " ").Replace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) {
		return 0, false
	}
	return load, true
}

func readDarwinMemoryAvailable() (uint64, bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	pageSizeRaw, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.pagesize").Output()
	if err != nil {
		return 0, false
	}
	pageSize, err := strconv.ParseUint(strings.TrimSpace(string(pageSizeRaw)), 10, 64)
	if err != nil || pageSize == 0 {
		return 0, false
	}
	vmRaw, err := exec.Command("/usr/bin/vm_stat").Output()
	if err != nil {
		return 0, false
	}
	return parseDarwinVMStatAvailable(string(vmRaw), pageSize)
}

func parseDarwinVMStatAvailable(raw string, pageSize uint64) (uint64, bool) {
	if pageSize == 0 {
		return 0, false
	}
	var pages uint64
	found := false
	for _, line := range strings.Split(raw, "\n") {
		name, value, ok := parseDarwinVMStatLine(line)
		if !ok {
			continue
		}
		switch name {
		case "Pages free", "Pages inactive", "Pages speculative":
			pages += value
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return pages * pageSize, true
}

func parseDarwinVMStatLine(line string) (string, uint64, bool) {
	left, right, ok := strings.Cut(line, ":")
	if !ok {
		return "", 0, false
	}
	right = strings.TrimSpace(strings.TrimSuffix(right, "."))
	right = strings.ReplaceAll(right, ",", "")
	value, err := strconv.ParseUint(right, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(left), value, true
}

func readCgroupMemoryAvailable() (uint64, bool) {
	for _, pair := range [][2]string{
		{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory.current"},
		{"/sys/fs/cgroup/memory/memory.limit_in_bytes", "/sys/fs/cgroup/memory/memory.usage_in_bytes"},
	} {
		limit, ok := readCgroupMemoryNumber(pair[0])
		if !ok || limit == 0 || limit > (1<<60) {
			continue
		}
		used, ok := readCgroupMemoryNumber(pair[1])
		if !ok {
			continue
		}
		if used >= limit {
			return 0, true
		}
		return limit - used, true
	}
	return 0, false
}

func readCgroupMemoryNumber(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "max" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (m *Manager) measuredAgentMemoryLocked() (uint64, int) {
	var total uint64
	var samples int
	for _, agent := range m.agents {
		if rss, ok := agentRSSBytes(agent); ok && rss > 0 {
			total += rss
			samples++
		}
	}
	for _, entry := range m.threadAgents {
		if entry == nil {
			continue
		}
		if rss, ok := agentRSSBytes(entry.agent); ok && rss > 0 {
			total += rss
			samples++
		}
	}
	if samples == 0 {
		return 0, 0
	}
	return total / uint64(samples), samples
}

func agentRSSBytes(agent interface{ Pid() int }) (rss uint64, ok bool) {
	if agent == nil {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			rss = 0
			ok = false
		}
	}()
	pid := agent.Pid()
	if pid <= 0 {
		return 0, false
	}
	if rss, ok := procStatusRSSBytes(pid); ok {
		return rss, true
	}
	return psGroupRSSBytes(pid)
}

func procStatusRSSBytes(pid int) (uint64, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, false
			}
			return kb * 1024, true
		}
	}
	return 0, false
}

func psGroupRSSBytes(pid int) (uint64, bool) {
	out, err := exec.Command("ps", "-o", "rss=", "-g", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	var total uint64
	var samples int
	for _, field := range strings.Fields(string(out)) {
		kb, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			continue
		}
		total += kb * 1024
		samples++
	}
	return total, samples > 0
}
func (m *Manager) agentCapacityStatusLocked(snapshot agentCapacitySnapshot) agentCapacityStatus {
	active := len(m.agents) + len(m.threadAgents) + m.tempAgents
	cpus := snapshot.CPUs
	if cpus <= 0 {
		cpus = 1
	}
	fallbackMax := cpus * 3
	if fallbackMax < 1 {
		fallbackMax = 1
	}
	estimated := snapshot.MeasuredAgentMemoryBytes
	samples := snapshot.MeasuredAgentSamples
	if measured, measuredSamples := m.measuredAgentMemoryLocked(); measuredSamples > 0 {
		estimated = measured
		samples = measuredSamples
	}
	if estimated == 0 {
		estimated = agentEstimatedMemoryBytes
	}
	st := agentCapacityStatus{
		ActiveAgents:         active,
		TempAgents:           m.tempAgents,
		CPUs:                 cpus,
		Load1:                snapshot.Load1,
		LoadKnown:            snapshot.LoadKnown,
		MemoryFree:           snapshot.MemoryAvailableBytes,
		MemoryKnown:          snapshot.MemoryKnown,
		FallbackMax:          fallbackMax,
		EstimatedAgentMemory: estimated,
		MeasuredAgentSamples: samples,
		Allowed:              true,
	}
	if snapshot.MemoryKnown && snapshot.MemoryAvailableBytes < agentMinFreeMemoryBytes+estimated {
		st.Allowed = false
		st.Reason = "memory"
		return st
	}
	if snapshot.LoadKnown && snapshot.Load1 >= float64(cpus)*agentMaxLoadPerCPU {
		st.Allowed = false
		st.Reason = "cpu"
		return st
	}
	if !snapshot.MemoryKnown && !snapshot.LoadKnown && active >= fallbackMax {
		st.Allowed = false
		st.Reason = "fallback_active_limit"
		return st
	}
	return st
}

func (m *Manager) agentCapacityModeEffective() string {
	if m == nil || strings.TrimSpace(m.agentCapacityMode) == "" {
		return "auto"
	}
	return strings.ToLower(strings.TrimSpace(m.agentCapacityMode))
}

func (m *Manager) agentCapacityEnabled() bool {
	return m.agentCapacityModeEffective() != "off"
}

func (m *Manager) agentResourceSnapshot() agentCapacitySnapshot {
	if m != nil && m.capacitySnapshot != nil {
		return m.capacitySnapshot()
	}
	return defaultAgentCapacitySnapshot()
}

func (m *Manager) currentAgentCapacityStatus() agentCapacityStatus {
	snapshot := m.agentResourceSnapshot()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agentCapacityStatusLocked(snapshot)
}

func (m *Manager) ensureAgentCapacityLocked(kind, target string) error {
	if !m.agentCapacityEnabled() {
		return nil
	}
	snapshot := m.agentResourceSnapshot()
	status := m.agentCapacityStatusLocked(snapshot)
	if status.Allowed {
		return nil
	}
	reclaimed := m.reclaimIdleAgentsLocked(kind, target)
	if reclaimed > 0 {
		snapshot = m.agentResourceSnapshot()
		status = m.agentCapacityStatusLocked(snapshot)
		if status.Allowed {
			log.Printf("[capacity] reclaimed idle agents=%d before starting %s %s", reclaimed, kind, target)
			return nil
		}
	}
	return &AgentCapacityError{Status: status, Reclaimed: reclaimed}
}

type idleAgentCandidate struct {
	kind         string
	id           string
	lastActivity time.Time
}

func (m *Manager) reclaimIdleAgentsLocked(kind, target string) int {
	candidates := make([]idleAgentCandidate, 0, len(m.agents)+len(m.threadAgents))
	for channelID, worker := range m.workers {
		if kind == "channel" && channelID == target {
			continue
		}
		if worker != nil && worker.HasWork() {
			continue
		}
		agent, ok := m.agents[channelID]
		if !ok {
			continue
		}
		if agent != nil && agent.IsBusy() {
			continue
		}
		candidates = append(candidates, idleAgentCandidate{kind: "channel", id: channelID, lastActivity: m.channelLastActivity[channelID]})
	}
	for threadID, entry := range m.threadAgents {
		if kind == "thread" && threadID == target {
			continue
		}
		if entry != nil && entry.worker != nil && entry.worker.HasWork() {
			continue
		}
		last := time.Time{}
		if entry != nil {
			if entry.agent != nil && entry.agent.IsBusy() {
				continue
			}
			last = entry.lastActivity
		}
		candidates = append(candidates, idleAgentCandidate{kind: "thread", id: threadID, lastActivity: last})
	}
	sort.Slice(candidates, func(i, j int) bool {
		li, lj := candidates[i].lastActivity, candidates[j].lastActivity
		if li.IsZero() && !lj.IsZero() {
			return true
		}
		if !li.IsZero() && lj.IsZero() {
			return false
		}
		return li.Before(lj)
	})
	reclaimed := 0
	for _, c := range candidates {
		switch c.kind {
		case "channel":
			m.stopChannel(c.id)
			log.Printf("[capacity] reclaimed idle channel agent ch-%s", c.id)
			reclaimed++
		case "thread":
			if m.stopThreadAgentLocked(c.id) {
				log.Printf("[capacity] reclaimed idle thread agent thread-%s", c.id)
				reclaimed++
			}
		}
		status := m.agentCapacityStatusLocked(m.agentResourceSnapshot())
		if status.Allowed {
			break
		}
	}
	return reclaimed
}

func (m *Manager) reserveTempAgentSlot(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureAgentCapacityLocked("temporary", name); err != nil {
		return err
	}
	m.tempAgents++
	return nil
}

func (m *Manager) releaseTempAgentSlot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tempAgents > 0 {
		m.tempAgents--
	}
}

func (st agentCapacityStatus) memoryText() string {
	if !st.MemoryKnown {
		return "unknown"
	}
	return fmt.Sprintf("free=%dMiB need=%dMiB next_agent=%dMiB measured_samples=%d", st.MemoryFree/1024/1024, (agentMinFreeMemoryBytes+st.EstimatedAgentMemory)/1024/1024, st.EstimatedAgentMemory/1024/1024, st.MeasuredAgentSamples)
}

func (st agentCapacityStatus) cpuText() string {
	if !st.LoadKnown {
		return fmt.Sprintf("cpus=%d fallback_max=%d", st.CPUs, st.FallbackMax)
	}
	return fmt.Sprintf("load1=%.2f limit=%.2f cpus=%d", st.Load1, float64(st.CPUs)*agentMaxLoadPerCPU, st.CPUs)
}

func threadAgentLimitDisplay(limit int) string {
	if limit <= 0 {
		return "dynamic"
	}
	return strconv.Itoa(limit)
}

func (m *Manager) doctorAgentCapacity() string {
	if !m.agentCapacityEnabled() {
		return L.Getf("doctor.agent_capacity", "off", 0, 0, "disabled", "disabled")
	}
	st := m.currentAgentCapacityStatus()
	state := "ok"
	if !st.Allowed {
		state = st.Reason
	}
	return L.Getf("doctor.agent_capacity", state, st.ActiveAgents, st.TempAgents, st.memoryText(), st.cpuText())
}
