import '@testing-library/jest-dom'

// Suppress CSS module warnings in test env
Object.defineProperty(window, 'CSS', { value: null })
Object.defineProperty(document, 'doctype', {
  value: '<!DOCTYPE html>',
})
