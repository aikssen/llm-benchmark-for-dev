package domain

// Message is the content to deliver.
//
// The same message is delivered to every recipient of a notification. Subject
// is only meaningful for channels that carry one (email, push); channels that
// do not are free to ignore it.
type Message struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}
