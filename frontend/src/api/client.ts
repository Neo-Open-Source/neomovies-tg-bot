import axios from 'axios';

export const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/api',
  headers: {
    'Content-Type': 'application/json',
  },
});

// Helper to build image URLs
export const getImageUrl = (path?: string) => {
  if (!path) return undefined;
  if (path.startsWith('http')) return path;
  // If you have an image proxy, adjust here
  return `${import.meta.env.VITE_API_URL || ''}${path}`;
};
