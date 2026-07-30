package a2a

import "fmt"

func TaskNatsMsgID(from AgentID, to AgentID, messageID MessageID) string {
	return fmt.Sprintf("task:%s:%s:%s", from, to, messageID)
}

func ControlNatsMsgID(from AgentID, executor AgentID, taskID TaskID, kind string, revision int64) string {
	return fmt.Sprintf("control:%s:%s:%s:%s:%d", from, executor, taskID, kind, revision)
}

func PreAcceptEventNatsMsgID(executor AgentID, delegator AgentID, messageID MessageID, kind string) string {
	return fmt.Sprintf("event:%s:%s:msg_%s:%s", executor, delegator, messageID, kind)
}

func AcceptedEventNatsMsgID(executor AgentID, delegator AgentID, taskID TaskID) string {
	return fmt.Sprintf("event:%s:%s:%s:accepted", executor, delegator, taskID)
}

func StatusEventNatsMsgID(executor AgentID, delegator AgentID, taskID TaskID, revision int64) string {
	return fmt.Sprintf("event:%s:%s:%s:status:%d", executor, delegator, taskID, revision)
}

func ArtifactEventNatsMsgID(executor AgentID, delegator AgentID, taskID TaskID, artifactID string, revision int64) string {
	return fmt.Sprintf("event:%s:%s:%s:artifact:%s:%d", executor, delegator, taskID, artifactID, revision)
}

func ResultEventNatsMsgID(executor AgentID, delegator AgentID, taskID TaskID) string {
	return fmt.Sprintf("event:%s:%s:%s:result", executor, delegator, taskID)
}
