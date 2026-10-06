/** Rendered markup families only. Hash suffixes change; semantic CSS module prefixes do not.
 * Fixtures are synthetic contracts, not a claim of compatibility with every live rollout. */
export const selectors = {
  page: '.page-playlist, .page-album, .page-users__tracks, [class*="PlaylistPage_root__"], [class*="AlbumPage_root__"], [class*="CollectionTracksPage_root__"]',
  list: '.d-track-list, [class*="CommonTrackList_root__"], [class*="PlaylistPage_tracks__"], [class*="AlbumPage_tracks__"], [class*="CollectionTracksPage_tracks__"]',
  row: '.d-track, [class*="CommonTrack_root__"]',
  title: '.d-track__title, [class*="CommonTrack_title__"]',
  artist: '.d-track__artists a, [class*="CommonTrack_artists__"] a',
  album: '.d-track__album a, [class*="CommonTrack_album__"] a',
  version: '.d-track__version, [class*="CommonTrack_version__"]',
  duration: '.d-track__duration, [class*="CommonTrack_duration__"]',
  heading: 'h1, .page-playlist__title, .page-album__title, [class*="PageHeader_title__"]',
  count: '.page-playlist__tracks-count, .page-album__tracks-count, [class*="PageHeader_tracksCount__"]',
  end: '.d-track-list__end, [class*="CommonTrackList_end__"]',
  busy: '[aria-busy="true"], [role="progressbar"]',
  excluded: 'aside, [role="complementary"], .page-playlist__recommendations, .page-album__recommendations, [class*="Recommendations_root__"], [class*="PlayerBar_root__"]',
  link: 'a[href]',
} as const;
