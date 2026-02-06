import { BrowserRouter as Router, Routes, Route } from 'react-router-dom';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import { LibraryPage } from './pages/LibraryPage';
import { ItemPage } from './pages/ItemPage';

const theme = createTheme({
  palette: {
    mode: 'dark',
    primary: {
      main: '#f05a5a',
    },
    secondary: {
      main: '#9aa0a6',
    },
    background: {
      default: '#2b2b2b',
      paper: '#1f1f1f',
    },
    text: {
      primary: '#f1f1f1',
      secondary: '#b7b7b7',
    },
  },
  shape: {
    borderRadius: 10,
  },
  typography: {
    fontFamily: '"Manrope", "Segoe UI", Tahoma, sans-serif',
    h4: {
      fontWeight: 800,
    },
    h6: {
      fontWeight: 700,
    },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          backgroundColor: '#2b2b2b',
        },
      },
    },
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: '#1f1f1f',
          borderRadius: 14,
          border: '1px solid rgba(255,255,255,0.06)',
          boxShadow: '0 6px 20px rgba(0,0,0,0.35)',
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: '#1f1f1f',
          borderRadius: 14,
          border: '1px solid rgba(255,255,255,0.06)',
          boxShadow: '0 8px 24px rgba(0,0,0,0.35)',
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          borderRadius: 8,
          backgroundColor: 'rgba(255,255,255,0.06)',
        },
        deleteIcon: {
          color: '#d0d0d0',
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          borderRadius: 10,
          textTransform: 'none',
          fontWeight: 600,
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          borderRadius: 10,
          backgroundColor: '#252525',
        },
        notchedOutline: {
          borderColor: 'rgba(255,255,255,0.08)',
        },
      },
    },
    MuiInputLabel: {
      styleOverrides: {
        root: {
          color: '#b7b7b7',
        },
      },
    },
    MuiPagination: {
      styleOverrides: {
        root: {
          '& .MuiPaginationItem-root': {
            color: '#d6d6d6',
          },
          '& .Mui-selected': {
            backgroundColor: '#3b82f6',
            color: '#ffffff',
          },
        },
      },
    },
  },
});

function App() {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Router>
        <Routes>
          <Route path="/" element={<LibraryPage />} />
          <Route path="/item/:kpid" element={<ItemPage />} />
        </Routes>
      </Router>
    </ThemeProvider>
  );
}

export default App;
