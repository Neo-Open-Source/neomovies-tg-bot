export interface LibraryItem {
  kp_id: number;
  type: 'movie' | 'series' | 'cartoon' | 'anime';
  title: string;
  year?: number;
  poster_url?: string;
  rating?: number;
  added_at?: string;
  overview?: string;
  genres?: string[];
  voice?: string;
  quality?: string;
  seasons?: SeasonMeta[];
  seasons_count?: number;
  episodes_count?: number;
  voices?: string[];
}

export type MovieDetails = LibraryItem;

export interface EpisodeMeta {
  number: number;
  voice?: string;
  quality?: string;
}

export interface SeasonMeta {
  number: number;
  episodes?: EpisodeMeta[];
}
