import {
  AppBar,
  Toolbar,
  Box,
  TextField,
  InputAdornment,
  FormControl,
  Select,
  MenuItem,
  Stack,
  Typography,
} from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';

interface TopBarProps {
  search: string;
  onSearchChange: (value: string) => void;
  type: string;
  sortBy: string;
  onTypeChange: (value: string) => void;
  onSortByChange: (value: string) => void;
}

export const TopBar = ({
  search,
  onSearchChange,
  type,
  sortBy,
  onTypeChange,
  onSortByChange,
}: TopBarProps) => {
  return (
    <AppBar
      position="sticky"
      elevation={0}
      sx={{
        backgroundColor: '#1c1c1c',
        borderBottom: '1px solid rgba(255,255,255,0.06)',
      }}
    >
      <Toolbar
        sx={{
          maxWidth: 1200,
          width: '100%',
          mx: 'auto',
          gap: 2,
          flexWrap: 'wrap',
        }}
      >
        <Box sx={{ flexGrow: 1, display: 'flex', justifyContent: 'center', minWidth: 260 }}>
          <TextField
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Поиск..."
            size="small"
            sx={{ width: { xs: '100%', sm: 420 } }}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon sx={{ color: 'text.secondary' }} />
                </InputAdornment>
              ),
            }}
          />
        </Box>

        <Stack direction="row" spacing={2} sx={{ ml: { xs: 0, sm: 'auto' } }}>
          <Box>
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 0.5 }}>
              Тип
            </Typography>
            <FormControl size="small" sx={{ minWidth: 160 }}>
              <Select value={type} onChange={(e) => onTypeChange(e.target.value)} displayEmpty>
                <MenuItem value="">Все</MenuItem>
                <MenuItem value="movie">Фильмы</MenuItem>
                <MenuItem value="series">Сериалы</MenuItem>
              </Select>
            </FormControl>
          </Box>
          <Box>
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 0.5 }}>
              Сортировка
            </Typography>
            <FormControl size="small" sx={{ minWidth: 200 }}>
              <Select value={sortBy} onChange={(e) => onSortByChange(e.target.value)} displayEmpty>
                <MenuItem value="added">По дате добавления</MenuItem>
                <MenuItem value="title">По названию</MenuItem>
              </Select>
            </FormControl>
          </Box>
        </Stack>
      </Toolbar>
    </AppBar>
  );
};
