package wake

// Model is the minimal installed-model contract consumed by a wake engine. Metadata, validation
// and storage are added by Task 19 without changing the engine constructor.
type Model struct {
	ID   string
	Path string
	Kind Kind
}
