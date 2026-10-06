/** Spotify rendered markup only. Synthetic fixture contracts, not live rollout guarantees. */
export const selectors = {
  page: '[data-testid="playlist-page"], [data-testid="album-page"], [data-testid="collection-page"], [data-testid="collection-tracks-page"]',
  main: 'main, [role="main"]',
  list: '[data-testid="playlist-tracklist"], [data-testid="track-list"], [role="grid"]',
  row: '[data-testid="tracklist-row"]',
  title: '[data-testid="internal-track-link"], [data-testid="track-name"]',
  link: 'a[href]',
  artistText: '[data-testid="track-artist"]',
  albumText: '[data-testid="track-album"]',
  duration: '[data-testid="track-duration"]',
  cells: '[role="gridcell"]',
  heading: 'h1',
  // Restrict fallback text scanning to the rendered entity header, never the whole page.
  count: '[data-testid="track-count"], [data-testid="entityTitle"] ~ span, [data-testid="entityTitle"] ~ * span, [data-testid="entity-subtitle"] span',
  end: '[data-testid="track-list-end"]',
  busy: '[aria-busy="true"], [role="progressbar"], [data-testid="loading-indicator"]',
  excluded: 'aside, [role="complementary"], [data-testid="recommendations"], [data-testid="now-playing-widget"], [data-testid="queue"]',
} as const;
