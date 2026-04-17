package errors

import (
	"net/http"

	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
)

type HttpAgentError struct {
	Status  int    `json:"status"`
	Err     error  `json:"-"`
	Message string `json:"message"`
	AgentID string `json:"agent_id"`
	UserID  string `json:"user_id"`
}

type HttpAgentErrorDecorator func(*HttpAgentError)

func (h *HttpAgentError) Error() string {
	return h.Message
}

func (h *HttpAgentError) Unwrap() error {
	return h.Err
}

func NewHttpAgentError(status int, handlers ...HttpAgentErrorDecorator) *HttpAgentError {
	err := &HttpAgentError{
		Status: status,
	}
	for _, h := range handlers {
		h(err)
	}
	return err
}

func NewServerSideError(msg string, err error, agent *types.Agent) *HttpAgentError {
	return NewHttpAgentError(http.StatusInternalServerError, func(hae *HttpAgentError) {
		hae.AgentID = agent.ID
		hae.UserID = agent.UserID
		hae.Err = err
		hae.Message = msg
	})
}

func NewBadRequestError(msg string, err error, agent *types.Agent) *HttpAgentError {
	return NewHttpAgentError(http.StatusBadRequest, func(hae *HttpAgentError) {
		hae.AgentID = agent.ID
		hae.UserID = agent.UserID
		hae.Err = err
		hae.Message = msg
	})
}
