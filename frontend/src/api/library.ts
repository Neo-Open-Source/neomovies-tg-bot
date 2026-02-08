import { apiClient } from './client';
import type { LibraryItem, MovieDetails } from '../types';

export const libraryAPI = {
  // Get list of all movies/series in the bot library
  getLibrary(limit = 200) {
    return apiClient.get<LibraryItem[]>(`/library?limit=${limit}`);
  },

  // Get detailed information about a specific item
  getItemDetails(kpid: number) {
    return apiClient.get<MovieDetails>(`/library/item?kp_id=${kpid}`);
  },
};
