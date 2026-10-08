package destination

import "musicgetter/internal/domain"

// Feature availability is separate from the guarantees that permit automatic writes.
type Capabilities struct {
	Search                   bool `json:"supports_search"`
	SupportsFavorites        bool `json:"supports_favorites"`
	SupportsPlaylists        bool `json:"supports_playlists"`
	SupportsAlbumCollections bool `json:"supports_album_collections"`
	SupportsBulkAdd          bool `json:"supports_bulk_add"`
	SupportsMembershipLookup bool `json:"supports_membership_lookup"`
	MaxBulkSize              int  `json:"max_bulk_size"`
	NativeIdempotency        bool `json:"native_idempotency"`
	// Set semantics including preexisting membership, concurrent external writers,
	// and retries after arbitrary timeout. Read-before-add cannot advertise this.
	AtomicEnsureMembership bool `json:"atomic_ensure_membership"`
	ReconcileOperations    bool `json:"reconcile_operations"`
	// CreatePlaylist must bind a key to one immutable request/result for the entire
	// lifetime of the collection, including concurrent calls and lost responses.
	IdempotentCreatePlaylist bool `json:"idempotent_create_playlist"`
}

func (c Capabilities) Supports(kind domain.DestinationCollectionKind) bool {
	switch kind {
	case domain.CollectionFavorites:
		return c.SupportsFavorites
	case domain.CollectionPlaylist:
		return c.SupportsPlaylists
	case domain.CollectionAlbum:
		return c.SupportsAlbumCollections
	}
	return false
}
func (c Capabilities) Validate() error {
	if c.MaxBulkSize < 0 || c.MaxBulkSize > 200 || c.SupportsBulkAdd && c.MaxBulkSize < 2 || !c.SupportsBulkAdd && c.MaxBulkSize > 1 || c.IdempotentCreatePlaylist && !c.SupportsPlaylists {
		return domain.ErrInvalid
	}
	return nil
}

// CheckAdapter verifies advertised Go ports, not the truth of remote guarantees.
// Real adapters must additionally pass the reusable behavioural contract suite.
func CheckAdapter(d Destination, c Capabilities) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Search {
		if _, ok := d.(TrackSearcher); !ok {
			return ErrUnsupported
		}
	}
	if c.SupportsFavorites {
		if _, ok := d.(Favorites); !ok {
			return ErrUnsupported
		}
	}
	if c.SupportsPlaylists {
		if _, ok := d.(Playlists); !ok {
			return ErrUnsupported
		}
	}
	if c.SupportsAlbumCollections {
		if _, ok := d.(Albums); !ok {
			return ErrUnsupported
		}
	}
	if c.SupportsMembershipLookup {
		if _, ok := d.(MembershipReader); !ok {
			return ErrUnsupported
		}
	}
	if c.ReconcileOperations {
		if _, ok := d.(OperationReconciler); !ok {
			return ErrUnsupported
		}
	}
	return nil
}
