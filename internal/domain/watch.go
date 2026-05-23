package domain

type Watch struct {
	UserID       int64
	URL          string
	LocalID      int
	Name         string
	CreatedAt    int64
	Paused       bool
	Bootstrapped bool
	PriceMin     *int
	PriceMax     *int
	IncludeKw    []string
	ExcludeKw    []string
}
