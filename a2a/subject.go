package a2a

import (
	"fmt"
	"strings"
)

type SubjectKind string

const (
	SubjectKindTask      SubjectKind = "task"
	SubjectKindControl   SubjectKind = "control"
	SubjectKindEvent     SubjectKind = "event"
	SubjectKindCard      SubjectKind = "card"
	SubjectKindHeartbeat SubjectKind = "heartbeat"
)

type Subject struct {
	Kind      SubjectKind
	From      AgentID
	To        AgentID
	Executor  AgentID
	Delegator AgentID
	Agent     AgentID
	MessageID MessageID
	TaskID    TaskID
	TaskKey   string
	Control   string
	EventKind string
	Instance  string
}

func ParseSubject(raw string) (Subject, error) {
	if strings.TrimSpace(raw) == "" {
		return Subject{}, fmt.Errorf("subject is required")
	}
	parts := strings.Split(raw, ".")
	for _, part := range parts {
		if part == "" || part == "*" || part == ">" || strings.ContainsAny(part, " /") {
			return Subject{}, fmt.Errorf("invalid subject token %q", part)
		}
	}
	if len(parts) < 3 || parts[0] != "a2a" || parts[1] != "v1" {
		return Subject{}, fmt.Errorf("subject must use a2a.v1 prefix")
	}
	if parts[2] == "pool" {
		return Subject{}, fmt.Errorf("pool dispatch is not supported in v1")
	}

	switch parts[2] {
	case "task":
		if len(parts) != 6 {
			return Subject{}, fmt.Errorf("task subject must be a2a.v1.task.<from>.<to>.<messageId>")
		}
		from, to, msg := AgentID(parts[3]), AgentID(parts[4]), MessageID(parts[5])
		if err := ValidateAgentID(from); err != nil {
			return Subject{}, fmt.Errorf("from: %w", err)
		}
		if err := ValidateAgentID(to); err != nil {
			return Subject{}, fmt.Errorf("to: %w", err)
		}
		if err := ValidateMessageID(msg); err != nil {
			return Subject{}, fmt.Errorf("messageId: %w", err)
		}
		return Subject{Kind: SubjectKindTask, From: from, To: to, MessageID: msg}, nil
	case "control":
		if len(parts) != 7 {
			return Subject{}, fmt.Errorf("control subject must be a2a.v1.control.<from>.<executor>.<taskId>.<kind>")
		}
		from, executor, task := AgentID(parts[3]), AgentID(parts[4]), TaskID(parts[5])
		if err := ValidateAgentID(from); err != nil {
			return Subject{}, fmt.Errorf("from: %w", err)
		}
		if err := ValidateAgentID(executor); err != nil {
			return Subject{}, fmt.Errorf("executor: %w", err)
		}
		if err := ValidateTaskID(task); err != nil {
			return Subject{}, fmt.Errorf("taskId: %w", err)
		}
		if err := validateSubjectToken("control kind", parts[6]); err != nil {
			return Subject{}, err
		}
		return Subject{Kind: SubjectKindControl, From: from, Executor: executor, To: executor, TaskID: task, Control: parts[6]}, nil
	case "event":
		if len(parts) != 7 {
			return Subject{}, fmt.Errorf("event subject must be a2a.v1.event.<executor>.<delegator>.<taskKey>.<kind>")
		}
		executor, delegator := AgentID(parts[3]), AgentID(parts[4])
		if err := ValidateAgentID(executor); err != nil {
			return Subject{}, fmt.Errorf("executor: %w", err)
		}
		if err := ValidateAgentID(delegator); err != nil {
			return Subject{}, fmt.Errorf("delegator: %w", err)
		}
		if err := validateSubjectToken("task key", parts[5]); err != nil {
			return Subject{}, err
		}
		if err := validateSubjectToken("event kind", parts[6]); err != nil {
			return Subject{}, err
		}
		return Subject{Kind: SubjectKindEvent, From: executor, To: delegator, Executor: executor, Delegator: delegator, TaskKey: parts[5], EventKind: parts[6]}, nil
	case "card":
		if len(parts) != 4 {
			return Subject{}, fmt.Errorf("card subject must be a2a.v1.card.<agent>")
		}
		agent := AgentID(parts[3])
		if err := ValidateAgentID(agent); err != nil {
			return Subject{}, fmt.Errorf("agent: %w", err)
		}
		return Subject{Kind: SubjectKindCard, From: agent, Agent: agent}, nil
	case "heartbeat":
		if len(parts) != 5 {
			return Subject{}, fmt.Errorf("heartbeat subject must be a2a.v1.heartbeat.<agent>.<instance>")
		}
		agent := AgentID(parts[3])
		if err := ValidateAgentID(agent); err != nil {
			return Subject{}, fmt.Errorf("agent: %w", err)
		}
		if err := validateSubjectToken("instance", parts[4]); err != nil {
			return Subject{}, err
		}
		return Subject{Kind: SubjectKindHeartbeat, From: agent, Agent: agent, Instance: parts[4]}, nil
	default:
		return Subject{}, fmt.Errorf("unsupported a2a subject kind %q", parts[2])
	}
}

func TaskSubject(from AgentID, to AgentID, messageID MessageID) string {
	return fmt.Sprintf("a2a.v1.task.%s.%s.%s", from, to, messageID)
}

func ControlSubject(from AgentID, executor AgentID, taskID TaskID, kind string) string {
	return fmt.Sprintf("a2a.v1.control.%s.%s.%s.%s", from, executor, taskID, kind)
}

func EventSubject(executor AgentID, delegator AgentID, taskKey string, kind string) string {
	return fmt.Sprintf("a2a.v1.event.%s.%s.%s.%s", executor, delegator, taskKey, kind)
}

func CardSubject(agent AgentID) string {
	return fmt.Sprintf("a2a.v1.card.%s", agent)
}

func HeartbeatSubject(agent AgentID, instance string) string {
	return fmt.Sprintf("a2a.v1.heartbeat.%s.%s", agent, instance)
}
