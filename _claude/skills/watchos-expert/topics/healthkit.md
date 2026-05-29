# HealthKit Integration

### Setup

1. Add HealthKit capability in Xcode
2. Add usage descriptions to Info.plist:
   - `NSHealthShareUsageDescription`
   - `NSHealthUpdateUsageDescription`

### Request Authorization

```swift
import HealthKit

class HealthManager: ObservableObject {
    let healthStore = HKHealthStore()

    func requestAuthorization() async throws {
        let typesToRead: Set<HKObjectType> = [
            HKObjectType.quantityType(forIdentifier: .heartRate)!,
            HKObjectType.quantityType(forIdentifier: .stepCount)!,
            HKObjectType.workoutType()
        ]

        let typesToWrite: Set<HKSampleType> = [
            HKObjectType.workoutType()
        ]

        try await healthStore.requestAuthorization(toShare: typesToWrite, read: typesToRead)
    }
}
```

### Query Data

```swift
func fetchHeartRate() async throws -> Double {
    let heartRateType = HKQuantityType.quantityType(forIdentifier: .heartRate)!
    let predicate = HKQuery.predicateForSamples(withStart: Date().addingTimeInterval(-3600), end: Date())
    let sortDescriptor = NSSortDescriptor(key: HKSampleSortIdentifierEndDate, ascending: false)

    return try await withCheckedThrowingContinuation { continuation in
        let query = HKSampleQuery(sampleType: heartRateType, predicate: predicate, limit: 1, sortDescriptors: [sortDescriptor]) { _, samples, error in
            if let error = error {
                continuation.resume(throwing: error)
                return
            }

            guard let sample = samples?.first as? HKQuantitySample else {
                continuation.resume(returning: 0)
                return
            }

            let heartRate = sample.quantity.doubleValue(for: HKUnit(from: "count/min"))
            continuation.resume(returning: heartRate)
        }

        healthStore.execute(query)
    }
}
```
