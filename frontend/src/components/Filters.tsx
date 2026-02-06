import {
  Box,
  Typography,
  FormControl,
  Select,
  MenuItem,
  Paper,
} from '@mui/material';

interface FiltersProps {
  type: string;
  sortBy: string;
  onTypeChange: (value: string) => void;
  onSortByChange: (value: string) => void;
}

export const Filters = ({
  type,
  sortBy,
  onTypeChange,
  onSortByChange,
}: FiltersProps) => {
  return (
    <Paper sx={{ p: 3, height: 'fit-content', position: 'sticky', top: 80 }}>
      <Box sx={{ mb: 3 }}>
        <Typography variant="body2" sx={{ fontWeight: 600, mb: 1, color: 'text.secondary' }}>
          Тип
        </Typography>
        <FormControl fullWidth size="small">
          <Select
            value={type}
            onChange={(e) => onTypeChange(e.target.value)}
            displayEmpty
          >
            <MenuItem value="">Все</MenuItem>
            <MenuItem value="movie">Фильмы</MenuItem>
            <MenuItem value="series">Сериалы</MenuItem>
          </Select>
        </FormControl>
      </Box>

      <Box sx={{ mb: 3 }}>
        <Typography variant="body2" sx={{ fontWeight: 600, mb: 1, color: 'text.secondary' }}>
          Сортировка
        </Typography>
        <FormControl fullWidth size="small">
          <Select
            value={sortBy}
            onChange={(e) => onSortByChange(e.target.value)}
            displayEmpty
          >
            <MenuItem value="added">По дате добавления</MenuItem>
            <MenuItem value="title">По названию</MenuItem>
          </Select>
        </FormControl>
      </Box>
    </Paper>
  );
};
