Feature: Round and game lifecycle
  The round ends when every player has stopped, busted, been frozen, or gone inactive.
  Scores accumulate across rounds. The game ends when one player has the strictly
  highest score at or above 200 at the end of a round.
  If multiple players tie at 200+, rounds continue until the tie is broken.

  Scenario: Round ends when all players have stopped
    Given Alice has [5]
    And Bob has [7]
    When Alice stops
    And Bob stops
    Then the round has ended

  Scenario: Scores accumulate across rounds
    Given Alice has [5]
    And Bob has [7]
    And Alice's total score is 10
    When Alice stops
    And Bob stops
    Then Alice's total score is 15

  Scenario: Game ends when a player reaches 200 points
    Given Alice has [10]
    And Bob has [5]
    And Alice's total score is 190
    When Alice stops
    And Bob stops
    Then the game is over
    And Alice is the winner

  Scenario: Tie at 200+ — the game continues until one player pulls ahead
    Given Alice has [10]
    And Bob has [10]
    And Alice's total score is 190
    And Bob's total score is 190
    When Alice stops
    And Bob stops
    Then the game is not over
    And the round has ended

  Scenario: Strictly highest score wins — both at 200+ but one is higher
    Given Alice has [12]
    And Bob has [5]
    And Alice's total score is 200
    And Bob's total score is 200
    When Alice stops
    And Bob stops
    Then the game is over
    And Alice is the winner

  Scenario: Busting near the win threshold does not score — game continues
    Given Alice has [5]
    And Bob has [3]
    And Alice's total score is 190
    And the deck is [5]
    When Alice draws
    And Bob stops
    Then Alice's total score is 190
    And the game is not over
