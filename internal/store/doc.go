// Package store reads and writes environment variables using a public-key
// encrypted append journal. The journal allows writes and key-only listing
// without decrypting existing values; .env import/export and variable-name
// validation are centralized here as well.
package store
