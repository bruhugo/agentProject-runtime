package docker

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
)

const stateFilePath = "state.json"

func CreateStateFile() error {
	if _, err := os.Stat(stateFilePath); os.IsNotExist(err) {
		return os.WriteFile(stateFilePath, []byte("[]"), 0644)
	}
	return nil
}

func ReadStateFile() ([]types.AgentState, error) {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state []types.AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	return state, nil
}

func writeStateFile(state []types.AgentState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	return os.WriteFile(stateFilePath, data, 0644)
}

func AddAgentToState(agent *types.Agent, container *types.Container) error {
	state, err := ReadStateFile()
	if err != nil {
		return err
	}

	for _, s := range state {
		if s.Agent.ID == agent.ID {
			return nil
		}
	}

	state = append(state, types.AgentState{
		Agent:      *agent,
		Container:  *container,
		Retries:    0,
		MaxRetries: 3,
	})

	return writeStateFile(state)
}

func UpdateAgentState(agent *types.Agent) error {
	state, err := ReadStateFile()
	if err != nil {
		return err
	}

	found := false
	for i, s := range state {
		if s.Agent.ID == agent.ID {
			state[i].Agent = *agent
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("agent %s not found in state file", agent.ID)
	}

	return writeStateFile(state)
}

func RemoveAgentFromState(agentID string) error {
	state, err := ReadStateFile()
	if err != nil {
		return err
	}

	var newState []types.AgentState
	for _, s := range state {
		if s.Agent.ID != agentID {
			newState = append(newState, s)
		}
	}

	return writeStateFile(newState)
}

func FindAgentStateFile(agentID string) (*types.AgentState, error) {
	states, err := ReadStateFile()
	if err != nil {
		return nil, fmt.Errorf("error reading from state file: %w", err)
	}

	for _, state := range states {
		return &state, nil
	}

	return nil, fmt.Errorf("no container running with agent id %s", agentID)
}
