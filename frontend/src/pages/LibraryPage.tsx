import {
  Box,
  CircularProgress,
  Alert,
  Pagination,
  Typography,
} from '@mui/material';
import { useState, useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { MovieCard } from '../components/MovieCard';
import { libraryAPI } from '../api/library';
import type { LibraryItem } from '../types';
import { TopBar } from '../components/TopBar';

export const LibraryPage = () => {
  const navigate = useNavigate();
  const [items, setItems] = useState<LibraryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [sortBy, setSortBy] = useState('added');
  const pageSize = 18;

  useEffect(() => {
    const loadLibrary = async () => {
      try {
        setLoading(true);
        const response = await libraryAPI.getLibrary(400);
        setItems(response.data);
      } catch (err: any) {
        setError(err.message || 'Failed to load library');
      } finally {
        setLoading(false);
      }
    };

    loadLibrary();
  }, []);

  const filtered = useMemo(() => {
    let result = items;

    if (search.trim()) {
      const query = search.trim().toLowerCase();
      result = result.filter((i) => i.title.toLowerCase().includes(query));
    }

    if (type) {
      result = result.filter((i) => i.type === type);
    }

    if (sortBy) {
      result = [...result].sort((a, b) => {
        if (sortBy === 'title') {
          return a.title.localeCompare(b.title);
        }

        const aDate = a.added_at ? new Date(a.added_at).getTime() : 0;
        const bDate = b.added_at ? new Date(b.added_at).getTime() : 0;
        return bDate - aDate;
      });
    }

    return result;
  }, [items, search, type, sortBy]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));

  const pageItems = useMemo(() => {
    const start = (page - 1) * pageSize;
    return filtered.slice(start, start + pageSize);
  }, [filtered, page]);

  useEffect(() => {
    // Reset page when filters change
    setPage(1);
  }, [search, type, sortBy]);

  const handleMovieClick = (item: LibraryItem) => {
    navigate(`/item/${item.kp_id}`);
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ py: 4 }}>
        <Alert severity="error">{error}</Alert>
      </Box>
    );
  }

  return (
    <Box>
      <TopBar
        search={search}
        onSearchChange={setSearch}
        type={type}
        sortBy={sortBy}
        onTypeChange={setType}
        onSortByChange={setSortBy}
      />

      <Box sx={{ maxWidth: 1200, mx: 'auto', px: { xs: 2, sm: 3 }, py: { xs: 3, sm: 4 } }}>
        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
            <CircularProgress size={48} />
          </Box>
        ) : error ? (
          <Alert severity="error" sx={{ mb: 3 }}>
            {error}
          </Alert>
        ) : filtered.length === 0 ? (
          <Box sx={{ textAlign: 'center', py: 8 }}>
            <Typography variant="h6" color="text.secondary">
              Библиотека пуста
            </Typography>
          </Box>
        ) : (
          <>
            <Box
              sx={{
                display: 'grid',
                gridTemplateColumns: {
                  xs: 'repeat(2, 1fr)',
                  sm: 'repeat(3, 1fr)',
                  md: 'repeat(4, 1fr)',
                  lg: 'repeat(4, 1fr)',
                  xl: 'repeat(5, 1fr)',
                },
                gap: 2.5,
                mb: 4,
              }}
            >
              {pageItems.map((item) => (
                <MovieCard key={item.kp_id} item={item} onClick={handleMovieClick} />
              ))}
            </Box>

            <Box sx={{ display: 'flex', justifyContent: 'center' }}>
              <Pagination
                count={pageCount}
                page={page}
                onChange={(_, p) => setPage(p)}
                color="primary"
              />
            </Box>
          </>
        )}
      </Box>
    </Box>
  );
};
