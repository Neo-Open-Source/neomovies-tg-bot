import {
  Container,
  Typography,
  Box,
  Chip,
  CircularProgress,
  Alert,
  Breadcrumbs,
  Link,
  Paper,
  Grid,
  Button,
} from '@mui/material';
import { useParams, Link as RouterLink } from 'react-router-dom';
import { useState, useEffect } from 'react';
import { libraryAPI } from '../api/library';
import type { MovieDetails } from '../types';

export const ItemPage = () => {
  const { kpid } = useParams<{ kpid: string }>();
  const [item, setItem] = useState<MovieDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadItem = async () => {
      if (!kpid) return;
      try {
        setLoading(true);
        const response = await libraryAPI.getItemDetails(Number(kpid));
        setItem(response.data);
      } catch (err: any) {
        setError(err.message || 'Failed to load item details');
      } finally {
        setLoading(false);
      }
    };

    loadItem();
  }, [kpid]);

  if (loading) {
    return (
      <Container sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
        <CircularProgress />
      </Container>
    );
  }

  if (error || !item) {
    return (
      <Container sx={{ py: 4 }}>
        <Alert severity="error">{error || 'Item not found'}</Alert>
      </Container>
    );
  }

  return (
    <Container maxWidth="lg" sx={{ py: 4 }}>
      {/* Breadcrumbs */}
      <Breadcrumbs sx={{ mb: 3 }}>
        <Link component={RouterLink} to="/" color="inherit" underline="hover">
          Кинотека
        </Link>
        <Typography color="text.primary">{item.title}</Typography>
      </Breadcrumbs>

      <Paper
        sx={{
          p: { xs: 2, md: 4 },
          backgroundColor: 'background.paper',
          borderRadius: 2,
          boxShadow: '0 16px 48px rgba(0,0,0,0.35)',
          border: '1px solid rgba(255,255,255,0.06)',
        }}
      >
        <Grid container spacing={4}>
          {/* Poster */}
          <Grid size={{ xs: 12, md: 4 }}>
            <Box
              sx={{
                width: '100%',
                aspectRatio: '2/3',
                backgroundColor: 'grey.900',
                borderRadius: 1,
                overflow: 'hidden',
                boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
                backgroundSize: 'cover',
                backgroundPosition: 'center',
                backgroundImage: item.poster_url
                  ? `url(${item.poster_url})`
                  : item.kp_id
                  ? `url(https://st.kp.yandex.net/images/film_big/${item.kp_id}.jpg)`
                  : 'none',
              }}
            />
          </Grid>

          {/* Info */}
          <Grid size={{ xs: 12, md: 8 }}>
            <Typography variant="h3" component="h1" gutterBottom fontWeight={700} sx={{ letterSpacing: -0.5 }}>
              {item.title}
            </Typography>

            <Box sx={{ mb: 3, display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'center' }}>
              <Chip
                label={item.type === 'movie' ? 'Фильм' : 'Сериал'}
                color="default"
                sx={{
                  fontSize: '0.8125rem',
                  fontWeight: 700,
                  bgcolor: 'rgba(255,255,255,0.06)',
                  color: 'text.primary',
                  height: 32,
                }}
              />
              {item.kp_id > 0 && (
                <Button
                  variant="contained"
                  component="a"
                  href={`https://t.me/neomovies_tg_bot?start=get_${item.kp_id}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  sx={{
                    textTransform: 'none',
                    fontWeight: 700,
                    bgcolor: '#e23b3b',
                    '&:hover': { bgcolor: '#c73232' },
                    height: 32,
                    minHeight: 32,
                    px: 2,
                    lineHeight: 1,
                  }}
                >
                  Смотреть
                </Button>
              )}
              {typeof item.seasons_count === 'number' && item.seasons_count > 0 && (
                <Chip
                  label={`${item.seasons_count} сезон(ов)`}
                  variant="outlined"
                  sx={{ borderColor: 'rgba(255,255,255,0.2)', color: 'text.primary', height: 32 }}
                />
              )}
              {typeof item.episodes_count === 'number' && item.episodes_count > 0 && (
                <Chip
                  label={`${item.episodes_count} эпизодов`}
                  variant="outlined"
                  sx={{ borderColor: 'rgba(255,255,255,0.2)', color: 'text.primary', height: 32 }}
                />
              )}
            </Box>

            {item.type === 'series' && (item.voice || item.quality) && (
              <Box sx={{ mb: 2, display: 'flex', gap: 1.5, flexWrap: 'wrap' }}>
                {item.voice && (
                  <Chip
                    label={`Озвучка: ${item.voice}`}
                    variant="outlined"
                    sx={{ borderColor: 'rgba(255,255,255,0.2)', color: 'text.primary', height: 30 }}
                  />
                )}
                {item.quality && (
                  <Chip
                    label={`Качество: ${item.quality}`}
                    variant="outlined"
                    sx={{ borderColor: 'rgba(255,255,255,0.2)', color: 'text.primary', height: 30 }}
                  />
                )}
              </Box>
            )}

            {/* Ratings */}
            {typeof item.rating === 'number' && item.rating > 0 && (
              <Box sx={{ mb: 3 }}>
                <Box>
                  <Typography variant="body2" color="text.secondary">
                    Рейтинг
                  </Typography>
                  <Typography variant="h5" fontWeight={700} sx={{ color: '#e23b3b' }}>
                    {item.rating.toFixed(1)}
                  </Typography>
                </Box>
              </Box>
            )}

            {/* Genres */}
            {item.genres && item.genres.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="body1" fontWeight={600} gutterBottom>
                  Жанры
                </Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {item.genres.map((genre) => (
                    <Chip
                      key={genre}
                      label={genre}
                      size="small"
                      variant="outlined"
                      sx={{ borderColor: 'rgba(255,255,255,0.18)', color: 'text.primary' }}
                    />
                  ))}
                </Box>
              </Box>
            )}

            {/* Description */}
            {item.overview && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="body1" fontWeight={600} gutterBottom>
                  Описание
                </Typography>
                <Typography variant="body1" sx={{ lineHeight: 1.6 }}>
                  {item.overview}
                </Typography>
              </Box>
            )}

            {/* Voices */}
            {item.voices && item.voices.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="body1" fontWeight={600} gutterBottom>
                  Доступные озвучки
                </Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {item.voices.map((voice) => (
                    <Chip key={voice} label={voice} size="small" color="secondary" />
                  ))}
                </Box>
              </Box>
            )}

            {/* Episode overrides */}
            {item.type === 'series' && item.seasons && item.seasons.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="body1" fontWeight={600} gutterBottom>
                  Отличия по сериям
                </Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {(() => {
                    const episodes = item.seasons.flatMap((s) =>
                      (s.episodes || []).map((ep) => ({ season: s.number, ...ep })),
                    );
                    if (episodes.length === 0) return null;

                    const countBy = (key: 'voice' | 'quality') => {
                      const map = new Map<string, number>();
                      for (const ep of episodes) {
                        const val = (ep[key] || '').trim();
                        if (!val) continue;
                        map.set(val, (map.get(val) || 0) + 1);
                      }
                      let top = '';
                      let max = 0;
                      for (const [k, v] of map.entries()) {
                        if (v > max) {
                          max = v;
                          top = k;
                        }
                      }
                      return top;
                    };

                    const baseVoice = (item.voice || '').trim() || countBy('voice');
                    const baseQuality = (item.quality || '').trim() || countBy('quality');

                    const chips = episodes.flatMap((ep) => {
                      const epVoice = (ep.voice || '').trim();
                      const epQuality = (ep.quality || '').trim();
                      const showVoice = epVoice && epVoice !== baseVoice;
                      const showQuality = epQuality && epQuality !== baseQuality;
                      if (!showVoice && !showQuality) return [];
                      const parts = [];
                      if (showVoice) parts.push(epVoice);
                      if (showQuality) parts.push(epQuality);
                      const label = `S${ep.season}E${ep.number}: ${parts.join(', ')}`;
                      return [
                        <Chip
                          key={`${ep.season}-${ep.number}`}
                          label={label}
                          size="small"
                          variant="outlined"
                          sx={{ borderColor: 'rgba(255,255,255,0.18)', color: 'text.primary' }}
                        />,
                      ];
                    });

                    if (chips.length === 0) return null;
                    return chips;
                  })()}
                </Box>
              </Box>
            )}
          </Grid>
        </Grid>
      </Paper>
    </Container>
  );
};
