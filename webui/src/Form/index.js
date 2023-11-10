import React, { useCallback, useEffect, useState } from "react";
import * as R from "ramda";
import styled from "styled-components/macro"; // eslint-disable-line no-unused-vars
import MButton from "@material-ui/core/Button";
import Container from "@material-ui/core/Container";
import * as C from "../Components";
import Select from "./Select";
import Slider from "./Slider";
import { parseQueryString } from "utils";
import { useTypeOptions } from "./options";
import { saveAs } from "file-saver";

const Button = styled(MButton)`
  margin: 1em;
`;

const ButtonSet = styled.div`
  display: flex;
  justify-content: flex-start;
  && > * {
    margin-right: 1em;
  }
`;

const Cell = styled.div`
  outline: 0px solid #eee;
  display: flex;
  align-items: flex-end;
  margin-bottom: 1em;
`;

const TwoColumns = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-gap: 1em 2em;
`;

const initialValues = {
  address: "",
  name: "",
  altName: "",
  city: "",
  state: "",
  providence: "",
  country: "",
  zip: "",
  limit: 10,
  q: "",
  sdnType: "individual",
  program: "",
  threshold: 99,
  // disabled ///////////
  // idNumber: "",
  // list: "All",
  score: 100
};

// eslint-disable-next-line
export default ({ onSubmit }) => {
  const [values, setValues] = React.useState(initialValues);

  const { values: typeOptionValues } = useTypeOptions();

  const handleChange = name => e => {
    const value = R.path(["target", "value"], e);
    setValues(values => R.assoc(name, value, values));
  };

  const handleChangeSlider = name => (e, value) => {
    setValues(values => R.assoc(name, value, values));
  };

  const [selectedFile, setSelectedFile] = useState(null);

  const handleFileSelect = (e) => {
    setSelectedFile(e.target.files[0]);
  };

  const handleBatchSearchClick = (e) => {  
    const formData = new FormData();
    if (selectedFile) {
      formData.append("csvFile", selectedFile);
    } else {
      alert("Please select a CSV file!");
      return;
    }
    formData.append("sdnType", values.sdnType);
    formData.append("min-match", values.score / 100);
    formData.append("threshold", values.threshold / 100);

    fetch("/search/batch", {
      method: "POST",
      body: formData
    })
      .then(response => {
        if (response.ok) {
          const warning = response.headers.get("X-Truncation-Warning");
          if (warning != null) {
            alert(warning);
          }
          return response.text();
        } else {
          throw new Error("Request failed with status: " + response.status);
        }
      })
      .then(data => {
        const blob = new Blob([data], { type: "text/csv" });
        const timestamp = new Date().toISOString().replace(/[-:]/g, "");
        const filename = `cc_ofac_search_results_${timestamp}.csv`;
        saveAs(blob, filename);
      })
      .catch(error => {
        console.error("Error:", error);
      });
  };
  
  // eslint-disable-next-line
  const submit = useCallback(onSubmit, []);
  useEffect(() => {
    const { search } = window.location;
    if (!search) {
      return;
    }
    setValues(values => {
      const newValues = R.mergeDeepRight(values, parseQueryString(search));
      submit(newValues);
      return newValues;
    });
  }, [submit]);

  return (
    <Container>
      <TwoColumns>
        <form
          onSubmit={e => {
            e.preventDefault();
            handleBatchSearchClick();
          }}
        >
          <C.Section>
            <C.SectionTitle>Batch Search</C.SectionTitle>
            <Cell style={{ paddingTop: "1em", width: "75%" }}>
              <Slider
                label="Minimum Name Score"
                id="score-slider"
                value={values["score"]}
                onChange={handleChangeSlider("score")}
                min={50}
                max={100}
                valueLabelDisplay="auto"
              />
              &nbsp;&nbsp;
              {values["score"]}
            </Cell>
            <Cell style={{ paddingTop: "1em", width: "75%" }}>
              <Slider
                label="Match Threshold"
                id="threshold-slider"
                value={values["threshold"]}
                onChange={handleChangeSlider("threshold")}
                min={50}
                max={100}
                valueLabelDisplay="auto"
              />
              &nbsp;&nbsp;
              {values["threshold"]}
            </Cell>
            <Cell style={{ width: "75%" }}>
              <Select
                label="Type"
                id="sdnType"
                value={values["sdnType"]}
                onChange={handleChange("sdnType")}
                options={typeOptionValues}
              />
            </Cell>
            <Cell style={{ paddingTop: "1em", width: "75%" }}>
              <label>CSV File:</label>
              &nbsp;&nbsp;
              <input type="file" onChange={handleFileSelect} />
            </Cell>
            <div style={{ display: "flex", justifyContent: "flex-end" }}>
              <Cell>
                <ButtonSet>
                  <Button variant="contained" color="primary" type="submit">
                    Search
                  </Button>
                </ButtonSet>
              </Cell>
            </div>
          </C.Section>
        </form>
        <Container>
          <h2>Instructions</h2>
          <p>
            <b>Minimum Name Score:</b> the fuzziness level for name matching<br/>
            <b>Match Threshold:</b> above this is considered a match, below a hit<br/>
            <b>Type:</b> the SDN type to search for (individual, entity, vessel, aircraft, all)<br/>
          </p>
          <p>Currently we only process the first <b>301</b> rows (header row + 300 records).</p>
          <p>The input CSV file must contain at least three columns:</p>
          <ol>
            <li><i>anything</i></li>
            <li>last name</li>
            <li>first name</li>
          </ol>
          <ul>
            <li>The second and third columns must contain names</li>
            <li>The first column can contain anything</li>
            <li>The input file can contain as many additional columns as you want</li>
            <li>Column names are not important, use whatever you want</li>
          </ul>
          <p>Example:</p>
          <pre>
            <code>
              {`id,last name,first name`}
              <br />
              {`123,Smith,John Jacob`}
            </code>
          </pre>
          <p>Search results are appended to each row, a new file will be downloaded.</p>
        </Container>
      </TwoColumns>
    </Container>
  );
};
