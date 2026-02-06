import { useMemo } from 'react';
import {
  Card,
  CardContent,
  Typography,
  Box,
  CardMedia,
  Rating,
} from '@mui/material';
import type { LibraryItem } from '../types';

interface MovieCardProps {
  item: LibraryItem;
  onClick?: (item: LibraryItem) => void;
}

export const MovieCard = ({ item, onClick }: MovieCardProps) => {
  // Use year field directly, fallback to extracting from title
  const year = useMemo(() => {
    if (item.year) return item.year;
    const match = item.title.match(/\((\d{4})\)\s*$|^(\d{4})\s+/);
    return match ? parseInt(match[1] || match[2]) : null;
  }, [item.title, item.year]);

  // Clean title without year
  const cleanTitle = useMemo(() => {
    return item.title.replace(/\s*\(\d{4}\)$/, '').replace(/^\d{4}\s+/, '');
  }, [item.title]);

  const rating = typeof item.rating === 'number' ? item.rating : 0;
  const kpID = item.kp_id;

  const handleClick = () => {
    if (onClick) {
      onClick(item);
    }
  };

  return (
    <Card
      onClick={handleClick}
      sx={{
        cursor: 'pointer',
        width: '100%',
        maxWidth: '100%',
        minWidth: 0,
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        transition: 'transform 0.2s ease, box-shadow 0.2s ease',
        position: 'relative',
        zIndex: 0,
        overflow: 'hidden',
        '&:hover': {
          transform: 'translateY(-8px)',
          boxShadow: '0 12px 28px rgba(0,0,0,0.45)',
          zIndex: 1,
        },
      }}
    >
      <Box sx={{ position: 'relative', height: 240, width: '100%', overflow: 'hidden', backgroundColor: '#111111' }}>
        <CardMedia
          component="img"
          height="240"
          image={item.poster_url || `https://st.kp.yandex.net/images/film_big/${kpID}.jpg`}
          alt={cleanTitle}
          loading="lazy"
          sx={{
            display: 'block',
            width: '100%',
            objectFit: 'cover',
            transform: 'scale(1.02)',
          }}
        />
      </Box>
      <CardContent sx={{ flexGrow: 1, minWidth: 0, backgroundColor: '#262626' }}>
        <Typography
          gutterBottom
          variant="h6"
          component="div"
          noWrap
          sx={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', fontSize: '1rem' }}
        >
          {cleanTitle}
        </Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Rating
            value={rating / 2}
            readOnly
            size="small"
            sx={{
              color: '#f5c542',
              '& .MuiRating-iconEmpty': {
                color: 'rgba(255,255,255,0.2)',
              },
            }}
          />
          <Typography variant="body2" color="text.secondary" sx={{ fontWeight: 600 }}>
            {rating.toFixed(1)}
          </Typography>
        </Box>
        {year && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
            {year}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
};
