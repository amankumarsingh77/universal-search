import React from 'react'
import {createRoot} from 'react-dom/client'
import './styles/globals.css'
import App from './App'
import { HideSuppressionProvider } from './hooks/useHideSuppression'

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <HideSuppressionProvider>
            <App/>
        </HideSuppressionProvider>
    </React.StrictMode>
)
