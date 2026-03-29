package main

import (
	"context"
	"sync"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var generatorPoint = func() secp256k1.JacobianPoint {
	priv := secp256k1.NewPrivateKey(scalarOne)
	pub := priv.PubKey()
	var point secp256k1.JacobianPoint
	pub.AsJacobian(&point)
	return point
}()

func fillBatchParallel(ctx context.Context, current secp256k1.ModNScalar, pubkeys []byte, privateKeys []byte, batchSize int, workers int) (secp256k1.ModNScalar, error) {
	if workers <= 1 || batchSize <= 1024 {
		return fillSequentialRange(ctx, current, pubkeys, privateKeys, batchSize)
	}

	if workers > batchSize {
		workers = batchSize
	}

	chunkSize := (batchSize + workers - 1) / workers
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		startIndex := workerIndex * chunkSize
		if startIndex >= batchSize {
			break
		}
		count := chunkSize
		if remaining := batchSize - startIndex; remaining < count {
			count = remaining
		}

		startScalar := scalarWithOffset(current, uint32(startIndex))
		pubChunk := pubkeys[startIndex*64 : (startIndex+count)*64]
		privChunk := privateKeys[startIndex*32 : (startIndex+count)*32]

		wg.Add(1)
		go func(start secp256k1.ModNScalar, localPub []byte, localPriv []byte, localCount int) {
			defer wg.Done()
			if err := fillSequentialRangeOnly(ctx, start, localPub, localPriv, localCount); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}(startScalar, pubChunk, privChunk, count)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return current, err
	default:
	}

	return scalarWithOffset(current, uint32(batchSize)), nil
}

func fillPubkeyBatchParallel(ctx context.Context, current secp256k1.ModNScalar, pubkeys []byte, batchSize int, workers int) (secp256k1.ModNScalar, error) {
	if workers <= 1 || batchSize <= 1024 {
		return fillSequentialPubkeyRange(ctx, current, pubkeys, batchSize)
	}

	if workers > batchSize {
		workers = batchSize
	}

	chunkSize := (batchSize + workers - 1) / workers
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		startIndex := workerIndex * chunkSize
		if startIndex >= batchSize {
			break
		}
		count := chunkSize
		if remaining := batchSize - startIndex; remaining < count {
			count = remaining
		}

		startScalar := scalarWithOffset(current, uint32(startIndex))
		pubChunk := pubkeys[startIndex*64 : (startIndex+count)*64]

		wg.Add(1)
		go func(start secp256k1.ModNScalar, localPub []byte, localCount int) {
			defer wg.Done()
			if err := fillSequentialPubkeyRangeOnly(ctx, start, localPub, localCount); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}(startScalar, pubChunk, count)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return current, err
	default:
	}

	return scalarWithOffset(current, uint32(batchSize)), nil
}

func fillSequentialRange(ctx context.Context, current secp256k1.ModNScalar, pubkeys []byte, privateKeys []byte, batchSize int) (secp256k1.ModNScalar, error) {
	next := current
	if err := fillSequentialRangeOnly(ctx, next, pubkeys, privateKeys, batchSize); err != nil {
		return current, err
	}
	return scalarWithOffset(current, uint32(batchSize)), nil
}

func fillSequentialPubkeyRange(ctx context.Context, current secp256k1.ModNScalar, pubkeys []byte, batchSize int) (secp256k1.ModNScalar, error) {
	next := current
	if err := fillSequentialPubkeyRangeOnly(ctx, next, pubkeys, batchSize); err != nil {
		return current, err
	}
	return scalarWithOffset(current, uint32(batchSize)), nil
}

func fillSequentialRangeOnly(ctx context.Context, start secp256k1.ModNScalar, pubkeys []byte, privateKeys []byte, count int) error {
	scalar := start
	var point secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(&scalar, &point)
	var nextPoint secp256k1.JacobianPoint

	for i := 0; i < count; i++ {
		if i%2048 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		affine := point
		affine.ToAffine()
		xBytes := affine.X.Bytes()
		yBytes := affine.Y.Bytes()
		privBytes := scalar.Bytes()

		copy(pubkeys[i*64:(i*64)+32], xBytes[:])
		copy(pubkeys[(i*64)+32:(i+1)*64], yBytes[:])
		copy(privateKeys[i*32:(i+1)*32], privBytes[:])

		scalar.Add(scalarOne)
		if scalar.IsZero() {
			scalar.Add(scalarOne)
			point.Set(&generatorPoint)
			continue
		}

		secp256k1.AddNonConst(&point, &generatorPoint, &nextPoint)
		point.Set(&nextPoint)
	}

	return nil
}

func fillSequentialPubkeyRangeOnly(ctx context.Context, start secp256k1.ModNScalar, pubkeys []byte, count int) error {
	scalar := start
	var point secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(&scalar, &point)
	var nextPoint secp256k1.JacobianPoint

	for i := 0; i < count; i++ {
		if i%2048 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		affine := point
		affine.ToAffine()
		xBytes := affine.X.Bytes()
		yBytes := affine.Y.Bytes()

		copy(pubkeys[i*64:(i*64)+32], xBytes[:])
		copy(pubkeys[(i*64)+32:(i+1)*64], yBytes[:])

		scalar.Add(scalarOne)
		if scalar.IsZero() {
			scalar.Add(scalarOne)
			point.Set(&generatorPoint)
			continue
		}

		secp256k1.AddNonConst(&point, &generatorPoint, &nextPoint)
		point.Set(&nextPoint)
	}

	return nil
}

func scalarWithOffset(base secp256k1.ModNScalar, offset uint32) secp256k1.ModNScalar {
	scalar := base
	if offset == 0 {
		return scalar
	}

	var delta secp256k1.ModNScalar
	delta.SetInt(offset)
	scalar.Add(&delta)
	if scalar.IsZero() {
		scalar.Add(scalarOne)
	}
	return scalar
}
