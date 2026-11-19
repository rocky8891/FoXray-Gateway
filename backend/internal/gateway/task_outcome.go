package gateway

import "fmt"

type partialTaskError struct{ message string }

func (e *partialTaskError) Error() string { return e.message }

func taskCompletionError(passed, total int, message string) error {
	if passed >= total && total > 0 {
		return nil
	}
	if passed > 0 {
		return &partialTaskError{message: message}
	}
	return fmt.Errorf("%s", message)
}
